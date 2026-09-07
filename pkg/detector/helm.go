package detector

// helmHookAnnotationKey marks a resource as a Helm lifecycle hook (pre-install,
// post-upgrade, etc.) — Helm manages its own scan-visible lifecycle for these
// (creating them right before a hook runs, deleting or retaining them per
// helm.sh/hook-delete-policy), so an unused-configmap-secret finding on a
// ConfigMap/Secret that only a hook Pod consumes would be a false positive,
// not a real orphan: Helm still owns and will reuse it on the next release.
const helmHookAnnotationKey = "helm.sh/hook"

// isHelmHook reports whether a resource is a Helm hook, regardless of which
// hook phase(s) its value names.
func isHelmHook(annotations map[string]string) bool {
	_, ok := annotations[helmHookAnnotationKey]
	return ok
}
