// Package cost estimates the AWS dollar cost of orphaned resources using a bundled,
// single-region static pricing table (ADR-0004, ADR-0007).
package cost

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed pricing.yaml
var defaultPricingYAML []byte

// PricingTable holds the $ rates used to estimate resource cost.
type PricingTable struct {
	Region                  string             `yaml:"region"`
	DefaultEBSVolumeType    string             `yaml:"default_ebs_volume_type"`
	EBSUSDPerGBMonth        map[string]float64 `yaml:"ebs_usd_per_gb_month"`
	LoadBalancerUSDPerMonth float64            `yaml:"load_balancer_usd_per_month"`
}

// LoadPricingTable parses a pricing table from YAML bytes (used for --pricing-file
// overrides as well as the bundled default table). It rejects a table whose
// DefaultEBSVolumeType has no matching rate — Estimator.EBSMonthlyCost falls back to
// that rate whenever a resource's own volume type is unknown, so if the default
// itself has no rate, every such estimate would silently come out as $0.00/month
// instead of failing loudly (the bug this validation exists to prevent). It rejects
// a zero/unset LoadBalancerUSDPerMonth for the same reason.
func LoadPricingTable(data []byte) (*PricingTable, error) {
	var table PricingTable
	if err := yaml.Unmarshal(data, &table); err != nil {
		return nil, err
	}
	if _, ok := table.EBSUSDPerGBMonth[table.DefaultEBSVolumeType]; !ok {
		return nil, fmt.Errorf(
			"pricing table: default_ebs_volume_type %q has no rate in ebs_usd_per_gb_month",
			table.DefaultEBSVolumeType,
		)
	}
	if table.LoadBalancerUSDPerMonth <= 0 {
		return nil, fmt.Errorf("pricing table: load_balancer_usd_per_month must be set and positive")
	}
	return &table, nil
}

// DefaultPricingTable returns the pricing table bundled into the binary.
func DefaultPricingTable() (*PricingTable, error) {
	return LoadPricingTable(defaultPricingYAML)
}
