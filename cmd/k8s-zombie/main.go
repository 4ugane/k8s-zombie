// Command k8s-zombie scans a Kubernetes cluster for orphaned resources and reports
// them with AWS cost estimates. See docs/superpowers/specs for the full design.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/4ugane/k8s-zombie/pkg/cli"
	"github.com/4ugane/k8s-zombie/pkg/cost"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "k8s-zombie",
		Short:         "Scan a Kubernetes cluster for orphaned resources burning budget",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newScanCmd())
	return root
}

func newScanCmd() *cobra.Command {
	var (
		kubeContext       string
		namespace         string
		excludeNamespaces []string
		output            string
		pricingFile       string
	)

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan the cluster and report orphaned resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := buildClientset(kubeContext)
			if err != nil {
				return err
			}
			table, err := loadPricingTable(pricingFile)
			if err != nil {
				return err
			}
			opts := cli.Options{
				Namespace:         namespace,
				ExcludeNamespaces: excludeNamespaces,
				Output:            output,
				PricingTable:      table,
			}
			return cli.Run(cmd.Context(), clientset, opts, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&kubeContext, "context", "", "kubeconfig context to use (default: current context)")
	cmd.Flags().StringVar(&namespace, "namespace", "", "only report findings in this namespace")
	cmd.Flags().StringSliceVar(&excludeNamespaces, "exclude-namespace", nil, "namespace to exclude from findings (repeatable)")
	cmd.Flags().StringVar(&output, "output", "table", "output format: table, json, markdown, or html")
	cmd.Flags().StringVar(&pricingFile, "pricing-file", "", "path to a custom pricing YAML file (default: bundled AWS pricing)")

	return cmd
}

// buildClientset loads a real Kubernetes client from the user's kubeconfig,
// honoring --context the same way kubectl does.
func buildClientset(kubeContext string) (kubernetes.Interface, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	return kubernetes.NewForConfig(config)
}

// loadPricingTable returns the bundled default pricing table, or a user-supplied
// override read from --pricing-file (ADR-0004/0007).
func loadPricingTable(path string) (*cost.PricingTable, error) {
	if path == "" {
		return cost.DefaultPricingTable()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading pricing file %s: %w", path, err)
	}
	return cost.LoadPricingTable(data)
}
