/**
 * Starting points, chosen because each one is a shape people actually need,
 * not because it demonstrates a feature. Every one is applyable as-is after
 * changing the namespace and the selector.
 */
export interface PolicyTemplate {
  id: string;
  name: string;
  blurb: string;
  yaml: string;
}

export const TEMPLATES: PolicyTemplate[] = [
  {
    id: "default-deny",
    name: "Default deny (namespace)",
    blurb: "The foundation. Nothing in or out until something else allows it.",
    yaml: `apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: default-deny
  namespace: default
spec:
  endpointSelector: {}
  ingress:
    - {}
  egress:
    - {}
`,
  },
  {
    id: "allow-between",
    name: "Allow one workload to reach another",
    blurb: "The rule you write immediately after default-deny breaks everything.",
    yaml: `apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: checkout-to-payments
  namespace: default
spec:
  endpointSelector:
    matchLabels:
      app: payments
  ingress:
    - fromEndpoints:
        - matchLabels:
            app: checkout
      toPorts:
        - ports:
            - port: "8443"
              protocol: TCP
`,
  },
  {
    id: "dns-allowlist",
    name: "DNS-aware egress allow-list",
    blurb: "Resolve names, then reach only the FQDNs on the list. Needs DNS visibility enabled.",
    yaml: `apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: egress-fqdn-allowlist
  namespace: default
spec:
  endpointSelector:
    matchLabels:
      app: my-app
  egress:
    - toEndpoints:
        - matchLabels:
            k8s:io.kubernetes.pod.namespace: kube-system
            k8s-app: kube-dns
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
          rules:
            dns:
              - matchPattern: "*"
    - toFQDNs:
        - matchName: "api.example.com"
      toPorts:
        - ports:
            - port: "443"
              protocol: TCP
`,
  },
  {
    id: "block-metadata",
    name: "Deny the cloud metadata service",
    blurb: "169.254.169.254 is how a compromised pod becomes a compromised cluster.",
    yaml: `apiVersion: cilium.io/v2
kind: CiliumClusterwideNetworkPolicy
metadata:
  name: deny-cloud-metadata
spec:
  endpointSelector: {}
  egressDeny:
    - toCIDR:
        - 169.254.169.254/32
`,
  },
  {
    id: "l7-http",
    name: "HTTP method and path allow-list",
    blurb: "Layer 7: allow GET on /healthz, refuse everything else on that port.",
    yaml: `apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: api-l7
  namespace: default
spec:
  endpointSelector:
    matchLabels:
      app: api
  ingress:
    - toPorts:
        - ports:
            - port: "8080"
              protocol: TCP
          rules:
            http:
              - method: GET
                path: "/healthz"
              - method: GET
                path: "/v1/.*"
`,
  },
  {
    id: "tetragon-exec",
    name: "Runtime: watch for a shell in a container",
    blurb: "Starts in monitor mode. Dry run it before you consider Sigkill.",
    yaml: `apiVersion: cilium.io/v1alpha1
kind: TracingPolicy
metadata:
  name: watch-container-shell
spec:
  kprobes:
    - call: security_bprm_check
      syscall: false
      args:
        - index: 0
          type: linux_binprm
      selectors:
        - matchArgs:
            - index: 0
              operator: Postfix
              values:
                - /bin/sh
                - /bin/bash
          matchActions:
            - action: Post
`,
  },
  {
    id: "tetragon-files",
    name: "Runtime: watch reads of sensitive files",
    blurb: "security_file_open needs no BPF-LSM, so it works on more kernels than the exec hooks.",
    yaml: `apiVersion: cilium.io/v1alpha1
kind: TracingPolicy
metadata:
  name: watch-sensitive-files
spec:
  kprobes:
    - call: security_file_open
      syscall: false
      return: true
      args:
        - index: 0
          type: file
      returnArg:
        index: 0
        type: int
      selectors:
        - matchArgs:
            - index: 0
              operator: Prefix
              values:
                - /etc/shadow
                - /var/run/secrets/kubernetes.io/serviceaccount/
          matchActions:
            - action: Post
`,
  },
  {
    id: "k8s-netpol",
    name: "Native NetworkPolicy",
    blurb: "Portable across CNIs. Less expressive than Cilium's, but it moves with the workload.",
    yaml: `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: deny-all-ingress
  namespace: default
spec:
  podSelector: {}
  policyTypes:
    - Ingress
`,
  },
];
