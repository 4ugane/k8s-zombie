package cost_test

import (
	"testing"

	"github.com/4ugane/k8s-zombie/pkg/cost"
)

func TestLoadPricingTable_InvalidYAMLReturnsError(t *testing.T) {
	_, err := cost.LoadPricingTable([]byte("not: [valid yaml"))
	if err == nil {
		t.Fatal("want an error for malformed YAML, got nil")
	}
}

func TestLoadPricingTable_MissingDefaultVolumeTypeRateReturnsError(t *testing.T) {
	// default_ebs_volume_type names "gp3", but gp3 has no rate in the map — this
	// would otherwise silently make every cost estimate come out as $0.00/month.
	data := []byte(`
region: us-east-1
default_ebs_volume_type: gp3
ebs_usd_per_gb_month:
  io1: 0.125
`)
	_, err := cost.LoadPricingTable(data)
	if err == nil {
		t.Fatal("want an error when default_ebs_volume_type has no matching rate, got nil")
	}
}

func TestLoadPricingTable_MissingLoadBalancerRateReturnsError(t *testing.T) {
	// A zero/unset load_balancer_usd_per_month would silently price every
	// LoadBalancer-type zero-endpoint Service finding at $0.00/month.
	data := []byte(`
region: us-east-1
default_ebs_volume_type: gp3
ebs_usd_per_gb_month:
  gp3: 0.08
`)
	_, err := cost.LoadPricingTable(data)
	if err == nil {
		t.Fatal("want an error when load_balancer_usd_per_month is unset, got nil")
	}
}

func TestLoadPricingTable_ValidTableLoadsWithoutError(t *testing.T) {
	data := []byte(`
region: us-east-1
default_ebs_volume_type: gp3
ebs_usd_per_gb_month:
  gp3: 0.08
load_balancer_usd_per_month: 16.43
`)
	table, err := cost.LoadPricingTable(data)
	if err != nil {
		t.Fatalf("LoadPricingTable() error = %v", err)
	}
	if table.Region != "us-east-1" {
		t.Errorf("want Region=us-east-1, got %q", table.Region)
	}
}
