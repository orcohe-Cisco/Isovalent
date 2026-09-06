package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// ServerVersion returns the kube-apiserver's reported gitVersion (e.g. "v1.31.2").
func (c *Client) ServerVersion(ctx context.Context) (string, error) {
	data, err := c.Do(ctx, "GET", "/version", "", nil)
	if err != nil {
		return "", err
	}
	var v struct {
		GitVersion string `json:"gitVersion"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", err
	}
	if v.GitVersion == "" {
		return "", fmt.Errorf("apiserver returned no gitVersion")
	}
	return v.GitVersion, nil
}

// WorkloadImage returns the first container image of a DaemonSet or Deployment,
// trying each candidate namespace in order. Cilium and Tetragon move between
// kube-system and their own namespaces depending on how they were installed,
// so the caller passes every plausible location rather than guessing one.
//
// resource is "daemonsets" or "deployments".
func (c *Client) WorkloadImage(ctx context.Context, resource string, namespaces []string, name string) (string, string, error) {
	var lastErr error
	for _, ns := range namespaces {
		p := fmt.Sprintf("/apis/apps/v1/namespaces/%s/%s/%s",
			url.PathEscape(ns), resource, url.PathEscape(name))
		data, err := c.Do(ctx, "GET", p, "", nil)
		if err != nil {
			lastErr = err
			continue
		}
		var obj struct {
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Name  string `json:"name"`
							Image string `json:"image"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(data, &obj); err != nil {
			lastErr = err
			continue
		}
		containers := obj.Spec.Template.Spec.Containers
		if len(containers) == 0 {
			lastErr = fmt.Errorf("%s/%s in %s has no containers", resource, name, ns)
			continue
		}
		// Prefer the container named after the workload; some charts put a
		// sidecar first.
		image := containers[0].Image
		for _, ct := range containers {
			if ct.Name == name {
				image = ct.Image
				break
			}
		}
		return image, ns, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%s/%s not found in %v", resource, name, namespaces)
	}
	return "", "", lastErr
}

// ImageTag extracts the version tag from a container image reference.
// Handles digests ("repo:tag@sha256:…") and registry ports ("host:5000/img:tag").
func ImageTag(image string) string {
	ref := image
	if at := strings.Index(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	slash := strings.LastIndex(ref, "/")
	colon := strings.LastIndex(ref, ":")
	if colon > slash {
		return ref[colon+1:]
	}
	return "" // untagged, or digest-only
}

// NamespaceInfo is a namespace the console can target.
type NamespaceInfo struct {
	Name      string            `json:"name"`
	Phase     string            `json:"phase,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Protected bool              `json:"protected"`
}

// Namespaces lists cluster namespaces so the UI can offer a picker instead of
// a free-text field. Typing a namespace name is how you write a policy that
// selects nothing and looks like it worked.
func (c *Client) Namespaces(ctx context.Context) ([]NamespaceInfo, error) {
	data, err := c.Do(ctx, "GET", "/api/v1/namespaces", "", nil)
	if err != nil {
		return nil, err
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	out := make([]NamespaceInfo, 0, len(list.Items))
	for _, it := range list.Items {
		out = append(out, NamespaceInfo{
			Name: it.Metadata.Name, Phase: it.Status.Phase, Labels: it.Metadata.Labels,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
