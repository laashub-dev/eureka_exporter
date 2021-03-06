package utils

import (
	"fmt"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/arkady-emelyanov/eureka_exporter/pkg/kube"
	"github.com/arkady-emelyanov/eureka_exporter/pkg/models"
)

const (
	inClusterEurekaUrlFmt  = "http://%s:%d/eureka/apps"
	outClusterEurekaUrlFmt = "http://localhost:8001/api/v1/namespaces/%s/services/%s:%d/proxy/eureka/apps"

	inClusterServiceUrlFmt  = "http://%s:%s%s"
	outClusterServiceUrlFmt = "http://localhost:8001/api/v1/namespaces/%s/pods/%s:%s/proxy%s"
)

// DiscoverServices returns list of Eureka services found across all namespaces
func DiscoverServices(namespace, selector string, t time.Duration, inCluster bool) ([]models.Endpoint, error) {
	svcLabelSelector := metav1.ListOptions{
		TimeoutSeconds: proto.Int64(int64(t.Seconds())),
		LabelSelector:  selector,
	}

	svcList, err := kube.GetClient().CoreV1().Services(namespace).List(svcLabelSelector)
	if err != nil {
		return nil, err
	}

	res := make([]models.Endpoint, len(svcList.Items))
	for i, s := range svcList.Items {
		if s.Spec.ClusterIP == "" {
			log.Warn().
				Str("name", s.Name).
				Str("namespace", s.Namespace).
				Msg("Eureka doesn't have ClusterIP, skipping...")
			continue
		}

		if len(s.Spec.Ports) > 1 {
			log.Warn().
				Str("name", s.Name).
				Str("namespace", s.Namespace).
				Int("ports", len(s.Spec.Ports)).
				Msg("Eureka has multiple ports in service spec, first one will be used")
		}
		for _, p := range s.Spec.Ports {
			context := models.Context{
				Namespace: s.Namespace,
				Name:      s.Name,
			}
			if inCluster {
				res[i] = models.Endpoint{
					Context: context,
					URL: fmt.Sprintf(
						inClusterEurekaUrlFmt,
						s.Spec.ClusterIP,
						p.Port,
					),
				}
			} else {
				res[i] = models.Endpoint{
					Context: context,
					URL: fmt.Sprintf(
						outClusterEurekaUrlFmt,
						s.Namespace,
						s.Name,
						p.Port,
					),
				}
			}
			break
		}
	}

	return res, nil
}

// FormatEndpoint used to construct ClusterURL for found resource
// for develop purpose, it may used to format links via kubectl proxy
// but since there are no way to tell how pod will be named from Eureka response,
// InstanceId field is used in fake_eureka.
func FormatEndpoint(app models.Instance, inCluster bool) *models.Endpoint {
	if app.Port.Enabled == false {
		log.Info().
			Str("namespace", app.Namespace).
			Str("name", app.Name).
			Msg("Insecure port disabled, skipping application")
		return nil
	}

	metricsUri := ""
	for _, m := range app.Metadata {
		if m.PrometheusURI != "" {
			metricsUri = m.PrometheusURI
			break
		}
	}
	if metricsUri == "" {
		log.Info().
			Str("namespace", app.Namespace).
			Str("name", app.Name).
			Msg("No Metadata/PrometheusURI found, skipping..")
		return nil
	}

	ctx := models.Context{
		Namespace:  app.Namespace,
		Name:       app.Name,
		InstanceId: app.InstanceId,
	}

	if inCluster {
		return &models.Endpoint{
			Context: ctx,
			URL: fmt.Sprintf(
				inClusterServiceUrlFmt,
				app.IpAddress,
				app.Port.Value,
				metricsUri,
			),
		}
	} else {
		return &models.Endpoint{
			Context: ctx,
			URL: fmt.Sprintf(
				outClusterServiceUrlFmt,
				app.Namespace,
				app.InstanceId,
				app.Port.Value,
				metricsUri,
			),
		}
	}
}
