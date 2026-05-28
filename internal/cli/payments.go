
package cli

import (
	"github.com/spf13/cobra"
)

func newPaymentsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "payments",
		Short:       "Get and create payments",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newPaymentsCancelPlanWithRedirectToStripeCmd(flags))
	cmd.AddCommand(newPaymentsGetSubscriptionInfoCmd(flags))
	cmd.AddCommand(newPaymentsReactivatePlanCmd(flags))
	return cmd
}
