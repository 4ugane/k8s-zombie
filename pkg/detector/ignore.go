package detector

// ignoreLabelKey is the opt-out label a resource can carry to be excluded from
// every detector's reporting, regardless of whether it would otherwise qualify as
// orphaned (design spec section 5).
const ignoreLabelKey = "k8s-zombie.io/ignore"

// isIgnored reports whether a resource has opted out of detection via
// k8s-zombie.io/ignore: "true".
func isIgnored(labels map[string]string) bool {
	return labels[ignoreLabelKey] == "true"
}
