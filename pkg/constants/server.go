package constants

const DefaultSocketPath = "/var/run/headcni/daemon.sock"
const DefaultTailscaleServiceName = "headcni01"
const DefaultTailscaleDaemonDir = "/var/run/headcni/"
const DefaultTailscaleDaemonSocketPath = "/var/run/headcni/headcni_tailscale.sock"
const DefaultTailscaleHostSocketPath = "/var/run/tailscale/tailscaled.sock"
const DefaultTailscaleDaemonStateDir = "/var/lib/headcni"
const DefaultTailscaleDaemonStateFile = "/var/lib/headcni/tailscaled.state"

// k8s cni default config
const DefaultCNIConfigDir = "/etc/cni/net.d"
const DefaultHeadCNIConfigFile = "10-headcni.conflist"
const DefaultCNIEnvFile = "/var/lib/headcni/env.yaml"
