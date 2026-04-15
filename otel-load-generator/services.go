package main

import (
	"fmt"
	"math/rand"
)

type ServiceDefinition struct {
	Name          string
	Namespace     string
	Version       string
	Language      string
	Team          string
	Tier          string
	Region        string
	AZ            string
	InstanceID    string
	ContainerID   string
	PodName       string
	NodeName      string
	Cluster       string
	DeploymentEnv string
	HostType      string
	ExtraAttrs    map[string]string
}

var (
	regions = []string{"us-east-1", "us-west-2", "eu-west-1", "ap-southeast-1"}

	azsByRegion = map[string][]string{
		"us-east-1":      {"us-east-1a", "us-east-1b", "us-east-1c"},
		"us-west-2":      {"us-west-2a", "us-west-2b", "us-west-2c"},
		"eu-west-1":      {"eu-west-1a", "eu-west-1b", "eu-west-1c"},
		"ap-southeast-1": {"ap-southeast-1a", "ap-southeast-1b"},
	}

	clusters  = []string{"prod-main", "prod-secondary", "prod-edge"}
	hostTypes = []string{"m5.xlarge", "c5.2xlarge", "r5.large", "t3.medium", "m6g.large", "c6g.xlarge", "r6g.2xlarge", "m5.2xlarge", "c5a.xlarge", "g4dn.xlarge"}
)

type serviceTemplate struct {
	Name      string
	Namespace string
	Team      string
	Tier      string
	Lang      string
}

var serviceTemplates = []serviceTemplate{
	{"api-gateway", "ingress", "platform", "critical", "go"},
	{"auth-service", "identity", "identity", "critical", "go"},
	{"user-service", "identity", "identity", "critical", "java"},
	{"session-manager", "identity", "identity", "critical", "go"},
	{"token-service", "identity", "identity", "standard", "rust"},
	{"payment-processor", "payments", "payments", "critical", "java"},
	{"payment-gateway", "payments", "payments", "critical", "java"},
	{"fraud-detector", "payments", "payments", "critical", "python"},
	{"billing-service", "payments", "payments", "standard", "java"},
	{"invoice-generator", "payments", "payments", "standard", "node"},
	{"product-catalog", "catalog", "catalog", "critical", "java"},
	{"inventory-service", "catalog", "catalog", "critical", "go"},
	{"pricing-engine", "catalog", "catalog", "standard", "python"},
	{"recommendation-engine", "catalog", "ml-inference", "standard", "python"},
	{"search-indexer", "search", "search", "standard", "java"},
	{"search-api", "search", "search", "critical", "go"},
	{"search-ranker", "search", "search", "standard", "python"},
	{"order-service", "fulfillment", "fulfillment", "critical", "java"},
	{"shipping-calculator", "fulfillment", "fulfillment", "standard", "go"},
	{"warehouse-manager", "fulfillment", "fulfillment", "standard", "java"},
	{"delivery-tracker", "fulfillment", "fulfillment", "standard", "node"},
	{"notification-router", "notifications", "notifications", "standard", "go"},
	{"email-sender", "notifications", "notifications", "standard", "node"},
	{"sms-gateway", "notifications", "notifications", "standard", "go"},
	{"push-service", "notifications", "notifications", "best-effort", "node"},
	{"event-bus", "platform", "platform", "critical", "rust"},
	{"config-service", "platform", "platform", "critical", "go"},
	{"feature-flags", "platform", "devtools", "standard", "go"},
	{"rate-limiter", "platform", "platform", "critical", "rust"},
	{"cache-proxy", "platform", "platform", "critical", "go"},
	{"analytics-collector", "analytics", "analytics", "standard", "go"},
	{"analytics-aggregator", "analytics", "analytics", "standard", "python"},
	{"clickstream-processor", "analytics", "analytics", "standard", "java"},
	{"ml-model-server", "ml", "ml-inference", "standard", "python"},
	{"ml-feature-store", "ml", "ml-inference", "standard", "python"},
	{"image-resizer", "media", "catalog", "best-effort", "go"},
	{"cdn-origin", "media", "platform", "standard", "rust"},
	{"review-service", "catalog", "catalog", "standard", "node"},
	{"cart-service", "fulfillment", "fulfillment", "critical", "go"},
	{"checkout-orchestrator", "fulfillment", "fulfillment", "critical", "java"},
}

func randomHex(n int) string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = hexChars[rand.Intn(16)]
	}
	return string(b)
}

func pick(arr []string) string {
	return arr[rand.Intn(len(arr))]
}

func runtimeName(lang string) string {
	switch lang {
	case "java":
		return "OpenJDK"
	case "python":
		return "CPython"
	case "node":
		return "Node.js"
	default:
		return lang
	}
}

func runtimeVersion(lang string) string {
	switch lang {
	case "java":
		return "21.0.2"
	case "python":
		return "3.12.1"
	case "node":
		return "20.11.0"
	case "go":
		return "1.22.0"
	case "rust":
		return "1.75.0"
	default:
		return "1.0.0"
	}
}

// ProbResourceAttr is a resource attribute with an independent probability of being set.
type ProbResourceAttr struct {
	Prob float64
	Key  string
	Gen  func(tmpl serviceTemplate, region, az, cluster, podName string) string
}

// resourceAttrPool defines ~150 possible resource attributes with varying probabilities.
func resourceAttrPool() []ProbResourceAttr {
	return []ProbResourceAttr{
		// ---- always present (0.95-1.0) ----
		{1.0, "cloud.provider", func(t serviceTemplate, r, az, c, p string) string { return "aws" }},
		{1.0, "cloud.platform", func(t serviceTemplate, r, az, c, p string) string { return "aws_ec2" }},
		{1.0, "cloud.region", func(t serviceTemplate, r, az, c, p string) string { return r }},
		{1.0, "cloud.availability_zone", func(t serviceTemplate, r, az, c, p string) string { return az }},
		{0.99, "cloud.account.id", func(t serviceTemplate, r, az, c, p string) string { return "123456789012" }},
		{0.99, "os.type", func(t serviceTemplate, r, az, c, p string) string { return "linux" }},
		{0.98, "os.description", func(t serviceTemplate, r, az, c, p string) string { return "Amazon Linux 2023" }},
		{0.98, "process.runtime.name", func(t serviceTemplate, r, az, c, p string) string { return runtimeName(t.Lang) }},
		{0.97, "process.runtime.version", func(t serviceTemplate, r, az, c, p string) string { return runtimeVersion(t.Lang) }},
		{0.97, "process.pid", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%d", rand.Intn(65535)) }},
		{0.96, "k8s.namespace.name", func(t serviceTemplate, r, az, c, p string) string { return t.Namespace }},
		{0.96, "k8s.deployment.name", func(t serviceTemplate, r, az, c, p string) string { return t.Name }},
		{0.95, "k8s.cluster.name", func(t serviceTemplate, r, az, c, p string) string { return c }},
		{0.95, "k8s.pod.uid", func(t serviceTemplate, r, az, c, p string) string {
			return fmt.Sprintf("%s-%s-%s-%s-%s", randomHex(8), randomHex(4), randomHex(4), randomHex(4), randomHex(12))
		}},

		// ---- near-always (0.85-0.94) ----
		{0.94, "deployment.environment", func(t serviceTemplate, r, az, c, p string) string { return "production" }},
		{0.93, "deployment.id", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("deploy-%s", randomHex(8)) }},
		{0.92, "telemetry.sdk.name", func(t serviceTemplate, r, az, c, p string) string { return "opentelemetry" }},
		{0.91, "telemetry.sdk.language", func(t serviceTemplate, r, az, c, p string) string { return t.Lang }},
		{0.90, "telemetry.sdk.version", func(t serviceTemplate, r, az, c, p string) string { return "1.9.0" }},
		{0.89, "build.commit_sha", func(t serviceTemplate, r, az, c, p string) string { return randomHex(40) }},
		{0.88, "build.branch", func(t serviceTemplate, r, az, c, p string) string { return "main" }},
		{0.87, "build.number", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%d", rand.Intn(10000)) }},
		{0.86, "company.cost_center", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("cc-%s", t.Team) }},
		{0.85, "company.business_unit", func(t serviceTemplate, r, az, c, p string) string { return t.Namespace }},

		// ---- common (0.60-0.84) ----
		{0.84, "sla.tier", func(t serviceTemplate, r, az, c, p string) string { return t.Tier }},
		{0.82, "k8s.replicaset.name", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%s-%s", t.Name, randomHex(10)) }},
		{0.80, "k8s.pod.labels.app", func(t serviceTemplate, r, az, c, p string) string { return t.Name }},
		{0.78, "k8s.pod.labels.version", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("v1.%d.%d", rand.Intn(30), rand.Intn(100)) }},
		{0.76, "k8s.pod.labels.team", func(t serviceTemplate, r, az, c, p string) string { return t.Team }},
		{0.74, "k8s.pod.labels.tier", func(t serviceTemplate, r, az, c, p string) string { return t.Tier }},
		{0.72, "k8s.pod.labels.part_of", func(t serviceTemplate, r, az, c, p string) string { return t.Namespace }},
		{0.70, "k8s.node.labels.instance_type", func(t serviceTemplate, r, az, c, p string) string { return pick(hostTypes) }},
		{0.68, "k8s.node.labels.topology_zone", func(t serviceTemplate, r, az, c, p string) string { return az }},
		{0.66, "process.command_line", func(t serviceTemplate, r, az, c, p string) string {
			return fmt.Sprintf("/app/%s --config /etc/%s/config.yaml --port %d", t.Name, t.Name, 8080+rand.Intn(20))
		}},
		{0.64, "process.executable.path", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("/app/%s", t.Name) }},
		{0.62, "process.owner", func(t serviceTemplate, r, az, c, p string) string { return "appuser" }},
		{0.60, "host.arch", func(t serviceTemplate, r, az, c, p string) string { return pick([]string{"amd64", "arm64"}) }},

		// ---- moderate (0.30-0.59) ----
		{0.58, "host.image.id", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("ami-%s", randomHex(17)) }},
		{0.56, "host.image.name", func(t serviceTemplate, r, az, c, p string) string { return "amzn2-ami-kernel-5.10" }},
		{0.54, "container.image.name", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("123456789012.dkr.ecr.%s.amazonaws.com/%s", r, t.Name) }},
		{0.52, "container.image.tag", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("v1.%d.%d-%s", rand.Intn(30), rand.Intn(100), randomHex(7)) }},
		{0.50, "container.image.digest", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("sha256:%s", randomHex(64)) }},
		{0.48, "container.runtime", func(t serviceTemplate, r, az, c, p string) string { return pick([]string{"containerd", "docker", "cri-o"}) }},
		{0.46, "k8s.pod.annotations.prometheus_io_scrape", func(t serviceTemplate, r, az, c, p string) string { return "true" }},
		{0.44, "k8s.pod.annotations.prometheus_io_port", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%d", 9090+rand.Intn(10)) }},
		{0.42, "k8s.pod.annotations.sidecar_istio_io_inject", func(t serviceTemplate, r, az, c, p string) string { return "true" }},
		{0.40, "k8s.container.name", func(t serviceTemplate, r, az, c, p string) string { return t.Name }},
		{0.38, "k8s.container.image.id", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("sha256:%s", randomHex(64)) }},
		{0.36, "cloud.resource_id", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("arn:aws:ec2:%s:123456789012:instance/i-%s", r, randomHex(17)) }},
		{0.34, "cloud.organization.id", func(t serviceTemplate, r, az, c, p string) string { return "org-abc123" }},
		{0.32, "process.runtime.description", func(t serviceTemplate, r, az, c, p string) string {
			return fmt.Sprintf("%s %s on linux/arm64", runtimeName(t.Lang), runtimeVersion(t.Lang))
		}},
		{0.30, "host.cpu.model.name", func(t serviceTemplate, r, az, c, p string) string {
			return pick([]string{"AWS Graviton3", "Intel Xeon Platinum 8375C", "AMD EPYC 7R13", "AWS Graviton2"})
		}},

		// ---- uncommon (0.10-0.29) ----
		{0.28, "host.cpu.count", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%d", pick([]string{"2", "4", "8", "16", "32", "64"})) }},
		{0.26, "host.memory.total_bytes", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%d", (1+rand.Intn(64))*1073741824) }},
		{0.24, "k8s.pod.labels.managed_by", func(t serviceTemplate, r, az, c, p string) string { return pick([]string{"helm", "argocd", "flux", "kustomize"}) }},
		{0.22, "k8s.pod.labels.chart_version", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%d.%d.%d", rand.Intn(10), rand.Intn(30), rand.Intn(100)) }},
		{0.20, "k8s.pod.annotations.config_hash", func(t serviceTemplate, r, az, c, p string) string { return randomHex(32) }},
		{0.18, "k8s.hpa.name", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%s-hpa", t.Name) }},
		{0.16, "k8s.hpa.current_replicas", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%d", randInt(1, 20)) }},
		{0.14, "k8s.hpa.desired_replicas", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%d", randInt(1, 20)) }},
		{0.12, "k8s.pod.labels.canary", func(t serviceTemplate, r, az, c, p string) string { return pick([]string{"true", "false"}) }},
		{0.10, "k8s.cronjob.name", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%s-cleanup", t.Name) }},
		{0.10, "cloud.vpc.id", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("vpc-%s", randomHex(17)) }},
		{0.10, "cloud.subnet.id", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("subnet-%s", randomHex(17)) }},

		// ---- rare (0.01-0.09) ----
		{0.09, "cloud.security_group.id", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("sg-%s", randomHex(17)) }},
		{0.08, "k8s.pod.annotations.vault_hashicorp_com_agent_inject", func(t serviceTemplate, r, az, c, p string) string { return "true" }},
		{0.07, "k8s.pod.annotations.vault_hashicorp_com_role", func(t serviceTemplate, r, az, c, p string) string { return t.Name }},
		{0.06, "k8s.service_account.name", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%s-sa", t.Name) }},
		{0.05, "k8s.pod.labels.cost_allocation_tag", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%s/%s", t.Team, t.Namespace) }},
		{0.05, "container.memory.limit_bytes", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%d", (1+rand.Intn(16))*268435456) }},
		{0.05, "container.cpu.limit_millicores", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("%d", pick([]string{"250", "500", "1000", "2000", "4000"})) }},
		{0.04, "k8s.pod.labels.pdb_min_available", func(t serviceTemplate, r, az, c, p string) string { return pick([]string{"1", "2", "50%"}) }},
		{0.04, "k8s.node.labels.spot_instance", func(t serviceTemplate, r, az, c, p string) string { return pick([]string{"true", "false"}) }},
		{0.03, "k8s.pod.annotations.linkerd_io_inject", func(t serviceTemplate, r, az, c, p string) string { return "enabled" }},
		{0.03, "cloud.placement_group", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("pg-%s", randomHex(8)) }},
		{0.02, "k8s.pod.labels.topology_spread_max_skew", func(t serviceTemplate, r, az, c, p string) string { return "1" }},
		{0.02, "k8s.pod.labels.priority_class", func(t serviceTemplate, r, az, c, p string) string { return pick([]string{"system-critical", "high-priority", "default", "low-priority"}) }},
		{0.02, "host.ebs.volume_id", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("vol-%s", randomHex(17)) }},
		{0.01, "k8s.pod.annotations.backup_schedule", func(t serviceTemplate, r, az, c, p string) string { return "0 2 * * *" }},
		{0.01, "k8s.pod.labels.gpu_type", func(t serviceTemplate, r, az, c, p string) string { return pick([]string{"nvidia-t4", "nvidia-a10g", "nvidia-a100", "none"}) }},
		{0.008, "k8s.pod.annotations.chaos_mesh_inject", func(t serviceTemplate, r, az, c, p string) string { return "true" }},
		{0.005, "cloud.dedicated_host_id", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("h-%s", randomHex(17)) }},
		{0.003, "k8s.pod.labels.feature_gate_experimental", func(t serviceTemplate, r, az, c, p string) string { return "true" }},
		{0.002, "host.maintenance_window", func(t serviceTemplate, r, az, c, p string) string { return "sun:02:00-sun:06:00" }},
		{0.001, "cloud.capacity_reservation_id", func(t serviceTemplate, r, az, c, p string) string { return fmt.Sprintf("cr-%s", randomHex(17)) }},
	}
}

var resPool = resourceAttrPool()

func GenerateServiceDefinitions() []ServiceDefinition {
	var services []ServiceDefinition

	for _, tmpl := range serviceTemplates {
		replicaCount := 2
		if tmpl.Tier == "critical" {
			replicaCount = 3
		}
		for r := 0; r < replicaCount; r++ {
			region := regions[r%len(regions)]
			az := pick(azsByRegion[region])
			cluster := pick(clusters)
			podName := fmt.Sprintf("%s-%s-%s", tmpl.Name, randomHex(8), randomHex(5))

			// Roll dice for each resource attribute
			extra := make(map[string]string)
			for _, pa := range resPool {
				if rand.Float64() < pa.Prob {
					extra[pa.Key] = pa.Gen(tmpl, region, az, cluster, podName)
				}
			}

			services = append(services, ServiceDefinition{
				Name:          tmpl.Name,
				Namespace:     tmpl.Namespace,
				Version:       fmt.Sprintf("1.%d.%d", rand.Intn(30), rand.Intn(100)),
				Language:      tmpl.Lang,
				Team:          tmpl.Team,
				Tier:          tmpl.Tier,
				Region:        region,
				AZ:            az,
				InstanceID:    fmt.Sprintf("i-%s", randomHex(16)),
				ContainerID:   randomHex(64),
				PodName:       podName,
				NodeName:      fmt.Sprintf("ip-10-%d-%d-%d.ec2.internal", rand.Intn(255), rand.Intn(255), rand.Intn(255)),
				Cluster:       cluster,
				DeploymentEnv: "production",
				HostType:      pick(hostTypes),
				ExtraAttrs:    extra,
			})
		}
	}

	return services
}
