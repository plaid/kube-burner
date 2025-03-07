// Copyright 2020 The Kube-burner Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/cloud-bulldozer/go-commons/version"
	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/burner"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	"github.com/cloud-bulldozer/kube-burner/pkg/util/metrics"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"

	uid "github.com/satori/go.uuid"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var binName = filepath.Base(os.Args[0])

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   binName,
	Short: "Burn a kubernetes cluster",
	Long: `Kube-burner 🔥

Tool aimed at stressing a kubernetes cluster by creating or deleting lots of objects.`,
}

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of kube-burner",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Version:", version.Version)
		fmt.Println("Git Commit:", version.GitCommit)
		fmt.Println("Build Date:", version.BuildDate)
		fmt.Println("Go Version:", version.GoVersion)
		fmt.Println("OS/Arch:", version.OsArch)
	},
}

var completionCmd = &cobra.Command{
	Use:   "completion",
	Short: "Generates completion scripts for bash shell",
	Long: `To load completion in the current shell run
. <(kube-burner completion)

To configure your bash shell to load completions for each session execute:

# kube-burner completion > /etc/bash_completion.d/kube-burner
	`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return rootCmd.GenBashCompletion(os.Stdout)
	},
}

func initCmd() *cobra.Command {
	var err error
	var url, metricsEndpoint, metricsProfile, alertProfile, configFile string
	var username, password, uuid, token, configMap, namespace, userMetadata string
	var skipTLSVerify bool
	var prometheusStep time.Duration
	var timeout time.Duration
	var rc int
	var metricsScraper metrics.Scraper
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Launch benchmark",
		PostRun: func(cmd *cobra.Command, args []string) {
			log.Info("👋 Exiting kube-burner ", uuid)
			os.Exit(rc)
		},
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if configMap != "" {
				metricsProfile, alertProfile, err = config.FetchConfigMap(configMap, namespace)
				if err != nil {
					log.Fatal(err.Error())
				}
				// We assume configFile is config.yml
				configFile = "config.yml"
			}
			f, err := util.ReadConfig(configFile)
			if err != nil {
				log.Fatalf("Error reading configuration file %s: %s", configFile, err)
			}
			configSpec, err := config.Parse(uuid, f)
			if err != nil {
				log.Fatalf("Config error: %s", err.Error())
			}
			if configSpec.GlobalConfig.IndexerConfig.Type != "" || alertProfile != "" {
				metricsScraper = metrics.ProcessMetricsScraperConfig(metrics.ScraperConfig{
					ConfigSpec:      configSpec,
					Password:        password,
					PrometheusStep:  prometheusStep,
					MetricsEndpoint: metricsEndpoint,
					MetricsProfile:  metricsProfile,
					AlertProfile:    alertProfile,
					SkipTLSVerify:   skipTLSVerify,
					URL:             url,
					Token:           token,
					Username:        username,
					UserMetaData:    userMetadata,
				})
			}
			rc, err = burner.Run(configSpec, metricsScraper.PrometheusClients, metricsScraper.AlertMs, metricsScraper.Indexer, timeout, metricsScraper.Metadata)
			if err != nil {
				log.Errorf(err.Error())
				os.Exit(rc)
			}
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", uid.NewV4().String(), "Benchmark UUID")
	cmd.Flags().StringVarP(&url, "prometheus-url", "u", "", "Prometheus URL")
	cmd.Flags().StringVarP(&token, "token", "t", "", "Prometheus Bearer token")
	cmd.Flags().StringVar(&username, "username", "", "Prometheus username for authentication")
	cmd.Flags().StringVarP(&password, "password", "p", "", "Prometheus password for basic authentication")
	cmd.Flags().StringVarP(&metricsProfile, "metrics-profile", "m", "", "Metrics profile file or URL")
	cmd.Flags().StringVarP(&metricsEndpoint, "metrics-endpoint", "e", "", "YAML file with a list of metric endpoints")
	cmd.Flags().StringVarP(&alertProfile, "alert-profile", "a", "", "Alert profile file or URL")
	cmd.Flags().BoolVar(&skipTLSVerify, "skip-tls-verify", true, "Verify prometheus TLS certificate")
	cmd.Flags().DurationVarP(&prometheusStep, "step", "s", 30*time.Second, "Prometheus step size")
	cmd.Flags().DurationVarP(&timeout, "timeout", "", 4*time.Hour, "Benchmark timeout")
	cmd.Flags().StringVarP(&configFile, "config", "c", "", "Config file path or URL")
	cmd.Flags().StringVarP(&configMap, "configmap", "", "", "Configmap holding all the configuration: config.yml, metrics.yml and alerts.yml. metrics and alerts are optional")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Namespace where the configmap is")
	cmd.MarkFlagsMutuallyExclusive("config", "configmap")
	cmd.Flags().StringVar(&userMetadata, "user-metadata", "", "User provided metadata file, in YAML format")
	cmd.Flags().SortFlags = false
	return cmd
}

func destroyCmd() *cobra.Command {
	var uuid string
	var timeout time.Duration
	var rc int
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy old namespaces labeled with the given UUID.",
		PostRun: func(cmd *cobra.Command, args []string) {
			log.Info("👋 Exiting kube-burner ", uuid)
			os.Exit(rc)
		},
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			listOptions := metav1.ListOptions{LabelSelector: fmt.Sprintf("kube-burner-uuid=%s", uuid)}
			clientSet, restConfig, err := config.GetClientSet(0, 0)
			if err != nil {
				log.Fatalf("Error creating clientSet: %s", err)
			}
			burner.ClientSet = clientSet
			burner.DynamicClient = dynamic.NewForConfigOrDie(restConfig)
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			burner.CleanupNamespaces(ctx, listOptions, true)
			burner.CleanupNonNamespacedResources(ctx, listOptions, true)
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", "", "UUID")
	cmd.Flags().DurationVarP(&timeout, "timeout", "", 4*time.Hour, "Deletion timeout")
	cmd.MarkFlagRequired("uuid")
	return cmd
}

func measureCmd() *cobra.Command {
	var uuid string
	var rawNamespaces string
	var selector string
	var configFile string
	var jobName string
	var userMetadata string
	var indexer *indexers.Indexer
	metadata := make(map[string]interface{})
	cmd := &cobra.Command{
		Use:   "measure",
		Short: "Take measurements for a given set of resources without running workload",
		PostRun: func(cmd *cobra.Command, args []string) {
			log.Info("👋 Exiting kube-burner ", uuid)
		},
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			f, err := util.ReadConfig(configFile)
			if err != nil {
				log.Fatalf("Error reading configuration file %s: %s", configFile, err)
			}
			configSpec, err := config.Parse(configFile, f)
			if err != nil {
				log.Fatal(err.Error())
			}
			if len(configSpec.Jobs) > 0 {
				log.Fatal("No jobs are allowed in a measure subcommand config file")
			}
			if configSpec.GlobalConfig.IndexerConfig.Type != "" {
				indexerConfig := configSpec.GlobalConfig.IndexerConfig
				log.Infof("📁 Creating indexer: %s", indexerConfig.Type)
				indexer, err = indexers.NewIndexer(indexerConfig)
				if err != nil {
					log.Fatalf("%v indexer: %v", indexerConfig.Type, err.Error())
				}
			}
			if userMetadata != "" {
				metadata, err = util.ReadUserMetadata(userMetadata)
				if err != nil {
					log.Fatalf("Error reading provided user metadata: %v", err)
				}
			}
			labelSelector, err := labels.Parse(selector)
			if err != nil {
				log.Fatalf("Invalid selector: %v", err)
			}
			namespaceLabels := make(map[string]string)
			labelRequirements, _ := labelSelector.Requirements()
			for _, req := range labelRequirements {
				namespaceLabels[req.Key()] = req.Values().List()[0]
			}
			log.Infof("%v", namespaceLabels)
			measurements.NewMeasurementFactory(configSpec, indexer, metadata)
			measurements.SetJobConfig(&config.Job{
				Name:            jobName,
				Namespace:       rawNamespaces,
				NamespaceLabels: namespaceLabels,
			})
			measurements.Collect()
			if err = measurements.Stop(); err != nil {
				log.Error(err.Error())
			}
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", "", "UUID")
	cmd.Flags().StringVar(&userMetadata, "user-metadata", "", "User provided metadata file, in YAML format")
	cmd.Flags().StringVarP(&configFile, "config", "c", "config.yml", "Config file path or URL")
	cmd.Flags().StringVarP(&jobName, "job-name", "j", "kube-burner-measure", "Measure job name")
	cmd.Flags().StringVarP(&rawNamespaces, "namespaces", "n", corev1.NamespaceAll, "comma-separated list of namespaces")
	cmd.Flags().StringVarP(&selector, "selector", "l", "", "namespace label selector. (e.g. -l key1=value1,key2=value2)")
	return cmd
}

func indexCmd() *cobra.Command {
	var url, metricsEndpoint, metricsProfile, jobName string
	var start, end int64
	var username, password, uuid, token, userMetadata string
	var esServer, esIndex, metricsDirectory string
	var configSpec config.Spec
	var skipTLSVerify bool
	var prometheusStep time.Duration
	var tarballName string
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Index kube-burner metrics",
		Long:  "If no other indexer is specified, local indexer is used by default",
		Args:  cobra.NoArgs,
		PostRun: func(cmd *cobra.Command, args []string) {
			log.Info("👋 Exiting kube-burner ", uuid)
		},
		Run: func(cmd *cobra.Command, args []string) {
			configSpec.GlobalConfig.UUID = uuid
			if esServer != "" && esIndex != "" {
				configSpec.GlobalConfig.IndexerConfig = indexers.IndexerConfig{
					Type:    indexers.ElasticIndexer,
					Servers: []string{esServer},
					Index:   esIndex,
				}
			} else {
				configSpec.GlobalConfig.IndexerConfig = indexers.IndexerConfig{
					Type:             indexers.LocalIndexer,
					MetricsDirectory: metricsDirectory,
				}
			}
			metricsScraper := metrics.ProcessMetricsScraperConfig(metrics.ScraperConfig{
				ConfigSpec:      configSpec,
				Password:        password,
				PrometheusStep:  prometheusStep,
				MetricsEndpoint: metricsEndpoint,
				MetricsProfile:  metricsProfile,
				SkipTLSVerify:   skipTLSVerify,
				URL:             url,
				Token:           token,
				Username:        username,
				UserMetaData:    userMetadata,
			})
			docsToIndex := make(map[string][]interface{})
			for _, prometheusClients := range metricsScraper.PrometheusClients {
				prometheusJob := prometheus.Job{
					Start: time.Unix(start, 0),
					End:   time.Unix(end, 0),
					JobConfig: config.Job{
						Name: jobName,
					},
				}
				prometheusClients.JobList = append(prometheusClients.JobList, prometheusJob)
				if err := prometheusClients.ScrapeJobsMetrics(docsToIndex); err != nil {
					log.Fatal(err)
				}
			}
			log.Infof("Indexing metrics with UUID %s", uuid)
			metrics.IndexDatapoints(docsToIndex, configSpec.GlobalConfig.IndexerConfig.Type, metricsScraper.Indexer)
			if configSpec.GlobalConfig.IndexerConfig.Type == indexers.LocalIndexer && tarballName != "" {
				if err := metrics.CreateTarball(configSpec.GlobalConfig.IndexerConfig, tarballName); err != nil {
					log.Fatal(err)
				}
			}
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", uid.NewV4().String(), "Benchmark UUID")
	cmd.Flags().StringVarP(&url, "prometheus-url", "u", "", "Prometheus URL")
	cmd.Flags().StringVarP(&token, "token", "t", "", "Prometheus Bearer token")
	cmd.Flags().StringVar(&username, "username", "", "Prometheus username for authentication")
	cmd.Flags().StringVarP(&password, "password", "p", "", "Prometheus password for basic authentication")
	cmd.Flags().StringVarP(&metricsProfile, "metrics-profile", "m", "metrics.yml", "Metrics profile file")
	cmd.Flags().StringVarP(&metricsEndpoint, "metrics-endpoint", "e", "", "YAML file with a list of metric endpoints")
	cmd.Flags().BoolVar(&skipTLSVerify, "skip-tls-verify", true, "Verify prometheus TLS certificate")
	cmd.Flags().DurationVarP(&prometheusStep, "step", "s", 30*time.Second, "Prometheus step size")
	cmd.Flags().Int64VarP(&start, "start", "", time.Now().Unix()-3600, "Epoch start time")
	cmd.Flags().Int64VarP(&end, "end", "", time.Now().Unix(), "Epoch end time")
	cmd.Flags().StringVarP(&jobName, "job-name", "j", "kube-burner-indexing", "Indexing job name")
	cmd.Flags().StringVar(&userMetadata, "user-metadata", "", "User provided metadata file, in YAML format")
	cmd.Flags().StringVar(&metricsDirectory, "metrics-directory", "collected-metrics", "Directory to dump the metrics files in, when using default local indexing")
	cmd.Flags().StringVar(&esServer, "es-server", "", "Elastic Search endpoint")
	cmd.Flags().StringVar(&esIndex, "es-index", "", "Elastic Search index")
	cmd.Flags().StringVar(&tarballName, "tarball-name", "", "Dump collected metrics into a tarball with the given name, requires local indexing")
	cmd.Flags().SortFlags = false
	return cmd
}

func importCmd() *cobra.Command {
	var tarball string
	var esServer, esIndex, metricsDirectory string
	var configSpec config.Spec
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import metrics tarball",
		Run: func(cmd *cobra.Command, args []string) {
			if esServer != "" && esIndex != "" {
				configSpec.GlobalConfig.IndexerConfig = indexers.IndexerConfig{
					Type:    indexers.ElasticIndexer,
					Servers: []string{esServer},
					Index:   esIndex,
				}
			} else {
				configSpec.GlobalConfig.IndexerConfig = indexers.IndexerConfig{
					Type:             indexers.LocalIndexer,
					MetricsDirectory: metricsDirectory,
				}
			}
			indexerConfig := configSpec.GlobalConfig.IndexerConfig
			log.Infof("📁 Creating indexer: %s", indexerConfig.Type)
			indexer, err := indexers.NewIndexer(indexerConfig)
			if err != nil {
				log.Fatal(err.Error())
			}
			err = metrics.ImportTarball(tarball, indexer, indexerConfig.MetricsDirectory)
			if err != nil {
				log.Fatal(err.Error())
			}
		},
	}
	cmd.Flags().StringVar(&tarball, "tarball", "", "Metrics tarball file")
	cmd.Flags().StringVar(&metricsDirectory, "metrics-directory", "collected-metrics", "Directory to dump the metrics files in, when using default local indexing")
	cmd.Flags().StringVar(&esServer, "es-server", "", "Elastic Search endpoint")
	cmd.Flags().StringVar(&esIndex, "es-index", "", "Elastic Search index")
	cmd.MarkFlagRequired("tarball")
	return cmd
}

func alertCmd() *cobra.Command {
	var configSpec config.Spec
	var err error
	var url, alertProfile, username, password, uuid, token string
	var esServer, esIndex, metricsDirectory string
	var start, end int64
	var skipTLSVerify bool
	var alertM *alerting.AlertManager
	var prometheusStep time.Duration
	var indexer *indexers.Indexer
	cmd := &cobra.Command{
		Use:   "check-alerts",
		Short: "Evaluate alerts for the given time range",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			configSpec.GlobalConfig.UUID = uuid
			if esServer != "" && esIndex != "" {
				configSpec.GlobalConfig.IndexerConfig = indexers.IndexerConfig{
					Type:    indexers.ElasticIndexer,
					Servers: []string{esServer},
					Index:   esIndex,
				}
			} else if metricsDirectory != "" {
				configSpec.GlobalConfig.IndexerConfig = indexers.IndexerConfig{
					Type:             indexers.LocalIndexer,
					MetricsDirectory: metricsDirectory,
				}
			}
			if configSpec.GlobalConfig.IndexerConfig.Type != "" {
				indexerConfig := configSpec.GlobalConfig.IndexerConfig
				log.Infof("📁 Creating indexer: %s", indexerConfig.Type)
				indexer, err = indexers.NewIndexer(indexerConfig)
				if err != nil {
					log.Fatal(err.Error())
				}
			}
			auth := prometheus.Auth{
				Username:      username,
				Password:      password,
				Token:         token,
				SkipTLSVerify: skipTLSVerify,
			}
			p, err := prometheus.NewPrometheusClient(configSpec, url, auth, prometheusStep, map[string]interface{}{}, false)
			if err != nil {
				log.Fatal(err)
			}
			startTime := time.Unix(start, 0)
			endTime := time.Unix(end, 0)
			if alertM, err = alerting.NewAlertManager(alertProfile, uuid, indexer, p, false); err != nil {
				log.Fatalf("Error creating alert manager: %s", err)
			}
			err = alertM.Evaluate(startTime, endTime)
			log.Info("👋 Exiting kube-burner ", uuid)
			if err != nil {
				os.Exit(1)
			}
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", uid.NewV4().String(), "Benchmark UUID")
	cmd.Flags().StringVarP(&url, "prometheus-url", "u", "", "Prometheus URL")
	cmd.Flags().StringVarP(&token, "token", "t", "", "Prometheus Bearer token")
	cmd.Flags().StringVar(&username, "username", "", "Prometheus username for authentication")
	cmd.Flags().StringVarP(&password, "password", "p", "", "Prometheus password for basic authentication")
	cmd.Flags().StringVarP(&alertProfile, "alert-profile", "a", "alerts.yaml", "Alert profile file or URL")
	cmd.Flags().BoolVar(&skipTLSVerify, "skip-tls-verify", true, "Verify prometheus TLS certificate")
	cmd.Flags().DurationVarP(&prometheusStep, "step", "s", 30*time.Second, "Prometheus step size")
	cmd.Flags().Int64VarP(&start, "start", "", time.Now().Unix()-3600, "Epoch start time")
	cmd.Flags().Int64VarP(&end, "end", "", time.Now().Unix(), "Epoch end time")
	cmd.Flags().StringVar(&metricsDirectory, "metrics-directory", "", "Directory to dump the alert files in, enables local indexing when specified")
	cmd.Flags().StringVar(&esServer, "es-server", "", "Elastic Search endpoint")
	cmd.Flags().StringVar(&esIndex, "es-index", "", "Elastic Search index")
	cmd.MarkFlagRequired("prometheus-url")
	cmd.MarkFlagRequired("alert-profile")
	cmd.Flags().SortFlags = false
	return cmd
}

// executes rootCmd
func main() {
	rootCmd.AddCommand(
		versionCmd,
		initCmd(),
		measureCmd(),
		destroyCmd(),
		indexCmd(),
		alertCmd(),
		importCmd(),
		openShiftCmd(),
	)
	logLevel := rootCmd.PersistentFlags().String("log-level", "info", "Allowed values: debug, info, warn, error, fatal")
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		log.SetReportCaller(true)
		formatter := &log.TextFormatter{
			TimestampFormat: "2006-01-02 15:04:05",
			FullTimestamp:   true,
			DisableColors:   true,
			CallerPrettyfier: func(f *runtime.Frame) (function string, file string) {
				return "", fmt.Sprintf("%s:%d", path.Base(f.File), f.Line)
			},
		}
		log.SetFormatter(formatter)
		lvl, err := log.ParseLevel(*logLevel)
		if err != nil {
			log.Fatalf("Unknown log level %s", *logLevel)
		}
		log.SetLevel(lvl)
	}
	rootCmd.AddCommand(completionCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
