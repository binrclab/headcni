package constants

const DefaultSocketPath = "/var/run/headcni/daemon.sock"
const DefaultTailscaleServiceName = "headcni01"
const DefaultTailscaleDaemonDir = "/var/run/headcni/"
const DefaultTailscaleDaemonSocketPath = "/var/run/headcni/headcni_tailscale.sock"
const DefaultTailscaleHostSocketPath = "/var/run/headcni/tailscale.sock"

// k8s cni default config
const DefaultCNIConfigDir = "/etc/cni/net.d"
const DefaultHeadCNIConfigFile = "10-headcni.conflist"
