package cost

// Estimator computes $ estimates from a PricingTable.
type Estimator struct {
	table *PricingTable
}

// NewEstimator wraps a PricingTable for cost lookups.
func NewEstimator(table *PricingTable) *Estimator {
	return &Estimator{table: table}
}

// EBSMonthlyCost estimates the monthly USD cost of an EBS volume of the given type
// and size. If volumeType has no rate in the table, it falls back to the table's
// DefaultEBSVolumeType (ADR-0007) rather than returning zero/nil.
func (e *Estimator) EBSMonthlyCost(volumeType string, sizeGB float64) float64 {
	rate, ok := e.table.EBSUSDPerGBMonth[volumeType]
	if !ok {
		rate = e.table.EBSUSDPerGBMonth[e.table.DefaultEBSVolumeType]
	}
	return rate * sizeGB
}

// LoadBalancerMonthlyCost estimates the monthly USD cost of a LoadBalancer-type
// Service using a single blended rate (ADR-0004/0007: v1 does not distinguish
// NLB/ALB/classic ELB or model usage-based LCU charges).
func (e *Estimator) LoadBalancerMonthlyCost() float64 {
	return e.table.LoadBalancerUSDPerMonth
}
