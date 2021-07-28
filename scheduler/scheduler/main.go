package main

//go:generate go-bindata -pkg "config" -o ./config/config.go config
//go:generate go fmt ./config/config.go

import (
	"flag"
	"fmt"
	"time"

	"github.com/apache/thrift/lib/go/thrift"
	log "github.com/sirupsen/logrus"

	"github.com/twitter/scoot/bazel"
	"github.com/twitter/scoot/cloud/cluster"
	"github.com/twitter/scoot/common"
	"github.com/twitter/scoot/common/endpoints"
	"github.com/twitter/scoot/common/log/hooks"
	"github.com/twitter/scoot/os/temp"
	"github.com/twitter/scoot/scheduler"
	"github.com/twitter/scoot/scheduler/scheduler/config"
)

func nopDurationKeyExtractor(id string) string {
	return id
}

func main() {
	log.AddHook(hooks.NewContextHook())

	// Set Flags Needed by this Server
	thriftAddr := flag.String("thrift_addr", scheduler.DefaultSched_Thrift, "Bind address for api server")
	httpAddr := flag.String("http_addr", scheduler.DefaultSched_HTTP, "Bind address for http server")
	grpcAddr := flag.String("grpc_addr", scheduler.DefaultSched_GRPC, "Bind address for grpc server")
	configFlag := flag.String("config", "local.memory", "Scheduler Config (either a filename like local.memory or JSON text")
	logLevelFlag := flag.String("log_level", "info", "Log everything at this level and above (error|info|debug)")
	grpcConns := flag.Int("max_grpc_conn", bazel.MaxSimultaneousConnections, "max grpc listener connections")
	grpcRate := flag.Int("max_grpc_rps", bazel.MaxRequestsPerSecond, "max grpc incoming requests per second")
	grpcBurst := flag.Int("max_grpc_rps_burst", bazel.MaxRequestsBurst, "max grpc incoming requests burst")
	grpcStreams := flag.Int("max_grpc_streams", bazel.MaxConcurrentStreams, "max grpc streams per client")
	grpcIdleMins := flag.Int("max_grpc_idle_mins", bazel.MaxConnIdleMins, "max grpc connection idle time")
	flag.Parse()

	level, err := log.ParseLevel(*logLevelFlag)
	if err != nil {
		log.Error(err)
		return
	}
	log.SetLevel(level)

	schedulerConfig, err := config.GetSchedulerConfig(*configFlag)
	if err != nil {
		panic(fmt.Errorf("error creating schedule server config.  Scheduler not started. %s", err))
	}

	thriftServerSocket, err := thrift.NewTServerSocket(*thriftAddr)
	if err != nil {
		panic(fmt.Errorf("error creating thrift server socket.  Scheduler not started. %s", err))
	}

	statsReceiver := endpoints.MakeStatsReceiver("scheduler").Precision(time.Millisecond)
	httpServer := endpoints.NewTwitterServer(endpoints.Addr(*httpAddr), statsReceiver, nil)

	bazelGRPCConfig := &bazel.GRPCConfig{
		GRPCAddr:          *grpcAddr,
		ListenerMaxConns:  *grpcConns,
		RateLimitPerSec:   *grpcRate,
		BurstLimitPerSec:  *grpcBurst,
		ConcurrentStreams: *grpcStreams,
		MaxConnIdleMins:   *grpcIdleMins,
	}

	tmpDir, err := temp.NewTempDir("", "sched")
	if err != nil {
		panic(fmt.Errorf("error getting temp dir.  Scheduler not started. %s", err))
	}

	var cluster *cluster.Cluster
	if schedulerConfig.Cluster.Type == "inMemory" {
		cmc := &config.ClusterMemoryConfig{
			Count: schedulerConfig.Cluster.Count,
		}
		cluster, err = cmc.Create()
	} else {
		clc := &config.ClusterLocalConfig{}
		cluster, err = clc.Create()
	}
	if err != nil {
		panic(fmt.Errorf("error creating cluster config.  Scheduler not started. %s", err))
	}

	log.Infof("Starting Cloud Scoot API Server & Scheduler on %s with %s", *thriftAddr, *configFlag)
	StartServer(schedulerConfig.SchedulerConfiguration, schedulerConfig.SagaLog, schedulerConfig.Workers, thriftServerSocket, &statsReceiver, common.DefaultClientTimeout, httpServer, bazelGRPCConfig,
		tmpDir, nil, nopDurationKeyExtractor, cluster)
}
