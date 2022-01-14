package main

import (
	"flag"
	"math/rand"
	"time"

	"github.com/champly/clustermanager/pkg/accept"
	"github.com/champly/clustermanager/pkg/kube"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	cmd := &cobra.Command{
		Use:          "accept",
		Short:        "ap",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printFlags(cmd.Flags())

			ctx := signals.SetupSignalHandler()

			if err := kube.InitManagerPlaneClusterClient(ctx); err != nil {
				return err
			}

			ctrl, err := accept.New(ctx)
			if err != nil {
				return err
			}
			return ctrl.Start()
		},
	}

	klog.InitFlags(flag.CommandLine)

	cmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	if err := cmd.Execute(); err != nil {
		klog.Errorf("Execute accept failed: %+v", err)
	}
}

func printFlags(flags *pflag.FlagSet) {
	flags.VisitAll(func(f *pflag.Flag) {
		klog.Infof("FLAG: --%s=%q", f.Name, f.Value)
	})
}
