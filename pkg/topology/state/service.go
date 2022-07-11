/*
Copyright (C) 2022 Traefik Labs

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published
by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/

package state

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func (f *Fetcher) getServices() (map[string]*Service, error) {
	services, err := f.k8s.Core().V1().Services().Lister().List(labels.Everything())
	if err != nil {
		return nil, err
	}

	svcs := make(map[string]*Service)
	for _, service := range services {
		var externalPorts []int

		// for BC reason we keep externalPorts.
		for _, port := range service.Spec.Ports {
			externalPorts = append(externalPorts, int(port.Port))
		}

		sort.Ints(externalPorts)

		var externalIPs []string
		if service.Spec.Type == corev1.ServiceTypeLoadBalancer {
			for _, ingress := range service.Status.LoadBalancer.Ingress {
				hostname := ingress.Hostname
				if hostname == "" {
					hostname = ingress.IP
				}
				externalIPs = append(externalIPs, hostname)
			}
		}

		sort.Strings(externalIPs)

		svcName := objectKey(service.Name, service.Namespace)
		svcs[svcName] = &Service{
			Name:          service.Name,
			Namespace:     service.Namespace,
			Annotations:   sanitizeAnnotations(service.Annotations),
			Selector:      service.Spec.Selector,
			Type:          service.Spec.Type,
			ExternalIPs:   externalIPs,
			ExternalPorts: externalPorts,
		}
	}

	return svcs, nil
}

// GetServiceLogs returns the logs from a service.
func (f *Fetcher) GetServiceLogs(ctx context.Context, namespace, name string, lines, maxLen int) ([]byte, error) {
	service, err := f.k8s.Core().V1().Services().Lister().Services(namespace).Get(name)
	if err != nil {
		return nil, fmt.Errorf("invalid service %s/%s: %w", name, namespace, err)
	}

	pods, err := f.k8s.Core().V1().Pods().Lister().Pods(namespace).List(labels.SelectorFromSet(service.Spec.Selector))
	if err != nil {
		return nil, fmt.Errorf("list pods for %s/%s: %w", namespace, name, err)
	}

	if len(pods) == 0 {
		return nil, nil
	}
	if len(pods) > lines {
		pods = pods[:lines]
	}

	buf := bytes.NewBuffer(make([]byte, 0, maxLen*lines))
	podLogOpts := corev1.PodLogOptions{Previous: false, TailLines: int64Ptr(int64(lines / len(pods)))}
	for _, pod := range pods {
		req := f.clientSet.CoreV1().Pods(service.Namespace).GetLogs(pod.Name, &podLogOpts)
		podLogs, logErr := req.Stream(ctx)
		if logErr != nil {
			return nil, fmt.Errorf("opening pod log stream: %w", logErr)
		}

		r := bufio.NewReader(podLogs)
		for {
			b, readErr := r.ReadBytes('\n')
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					writeBytes(buf, b, maxLen)
					break
				}
				return nil, err
			}

			writeBytes(buf, b, maxLen)
		}

		if err = podLogs.Close(); err != nil {
			return nil, fmt.Errorf("closing pod log stream: %w", err)
		}
	}

	return buf.Bytes(), nil
}

func writeBytes(buf *bytes.Buffer, b []byte, maxLen int) {
	switch {
	case len(b) == 0:
		return
	case len(b) > maxLen:
		b = b[:maxLen-1]
		b = append(b, '\n')
	case b[len(b)-1] != byte('\n'):
		b = append(b, '\n')
	}

	buf.Write(b)
}

func int64Ptr(v int64) *int64 {
	return &v
}
