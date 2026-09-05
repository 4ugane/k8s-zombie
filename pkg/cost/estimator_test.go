package cost_test

import (
	"testing"

	"github.com/4ugane/k8s-zombie/pkg/cost"
)

func testTable() *cost.PricingTable {
	return &cost.PricingTable{
		Region:               "us-east-1",
		DefaultEBSVolumeType: "gp3",
		EBSUSDPerGBMonth: map[string]float64{
			"gp3": 0.08,
			"io1": 0.125,
		},
		LoadBalancerUSDPerMonth: 16.43,
	}
}

func TestEBSMonthlyCost_KnownVolumeType(t *testing.T) {
	e := cost.NewEstimator(testTable())

	got := e.EBSMonthlyCost("io1", 100)

	want := 12.5
	if got != want {
		t.Errorf("EBSMonthlyCost(io1, 100) = %v, want %v", got, want)
	}
}

func TestEBSMonthlyCost_UnknownVolumeTypeFallsBackToDefault(t *testing.T) {
	e := cost.NewEstimator(testTable())

	got := e.EBSMonthlyCost("st1-not-in-table", 100)

	want := 8.0 // 100 GB * gp3 default rate (0.08)
	if got != want {
		t.Errorf("EBSMonthlyCost(unknown, 100) = %v, want %v", got, want)
	}
}

func TestLoadBalancerMonthlyCost_ReturnsConfiguredRate(t *testing.T) {
	e := cost.NewEstimator(testTable())

	got := e.LoadBalancerMonthlyCost()

	if got != 16.43 {
		t.Errorf("LoadBalancerMonthlyCost() = %v, want 16.43", got)
	}
}

func TestDefaultPricingTable_LoadsEmbeddedYAML(t *testing.T) {
	table, err := cost.DefaultPricingTable()
	if err != nil {
		t.Fatalf("DefaultPricingTable() error = %v", err)
	}
	if table.Region == "" {
		t.Error("want non-empty Region in default pricing table")
	}
	if _, ok := table.EBSUSDPerGBMonth[table.DefaultEBSVolumeType]; !ok {
		t.Errorf("DefaultEBSVolumeType %q has no rate in EBSUSDPerGBMonth", table.DefaultEBSVolumeType)
	}
}
