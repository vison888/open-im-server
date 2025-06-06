// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package startrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strconv"
	"syscall"
	"time"

	conf "github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/tools/discovery/etcd"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/jsonutil"
	"google.golang.org/grpc/status"

	kdisc "github.com/openimsdk/open-im-server/v3/pkg/common/discoveryregister"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mw"
	"github.com/openimsdk/tools/utils/network"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func getConfigRpcMaxRequestBody(value reflect.Value) *conf.MaxRequestBody {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		num := value.NumField()
		for i := 0; i < num; i++ {
			field := value.Field(i)
			if !field.CanInterface() {
				continue
			}
			for field.Kind() == reflect.Pointer {
				field = field.Elem()
			}
			switch elem := field.Interface().(type) {
			case conf.Share:
				return &elem.RPCMaxBodySize
			case conf.MaxRequestBody:
				return &elem
			}
			if field.Kind() == reflect.Struct {
				if elem := getConfigRpcMaxRequestBody(field); elem != nil {
					return elem
				}
			}
		}
	}
	return nil
}

// Start rpc server.
func Start[T any](ctx context.Context, discovery *conf.Discovery, prometheusConfig *conf.Prometheus, listenIP,
	registerIP string, autoSetPorts bool, rpcPorts []int, index int, rpcRegisterName string, share *conf.Share, config T,
	watchServiceNames []string,
	rpcFn func(ctx context.Context, config T, client discovery.SvcDiscoveryRegistry, server *grpc.Server) error,
	options ...grpc.ServerOption) error {

	maxRequestBody := &share.RPCMaxBodySize
	var clientOptions []grpc.DialOption
	if maxRequestBody != nil {
		if maxRequestBody.RequestMaxBodySize > 0 {
			options = append(options, grpc.MaxRecvMsgSize(maxRequestBody.RequestMaxBodySize))
			clientOptions = append(clientOptions, grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(maxRequestBody.RequestMaxBodySize)))
		}
		if maxRequestBody.ResponseMaxBodySize > 0 {
			options = append(options, grpc.MaxSendMsgSize(maxRequestBody.ResponseMaxBodySize))
			clientOptions = append(clientOptions, grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxRequestBody.ResponseMaxBodySize)))
		}
	}

	var (
		rpcTcpAddr     string
		netDone        = make(chan struct{}, 2)
		netErr         error
		prometheusPort int
	)

	registerIP, err := network.GetRpcRegisterIP(registerIP)
	if err != nil {
		return err
	}

	if !autoSetPorts {
		rpcPort, err := datautil.GetElemByIndex(rpcPorts, index)
		if err != nil {
			return err
		}
		rpcTcpAddr = net.JoinHostPort(network.GetListenIP(listenIP), strconv.Itoa(rpcPort))
	} else {
		rpcTcpAddr = net.JoinHostPort(network.GetListenIP(listenIP), "0")
	}

	getAutoPort := func() (net.Listener, int, error) {
		listener, err := net.Listen("tcp", rpcTcpAddr)
		if err != nil {
			return nil, 0, errs.WrapMsg(err, "listen err", "rpcTcpAddr", rpcTcpAddr)
		}
		_, portStr, _ := net.SplitHostPort(listener.Addr().String())
		port, _ := strconv.Atoi(portStr)
		return listener, port, nil
	}

	if autoSetPorts && discovery.Enable != conf.ETCD {
		return errs.New("only etcd support autoSetPorts", "rpcRegisterName", rpcRegisterName).Wrap()
	}
	client, err := kdisc.NewDiscoveryRegister(discovery, share, watchServiceNames)
	if err != nil {
		return err
	}

	defer client.Close()
	client.AddOption(mw.GrpcClient(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"LoadBalancingPolicy": "%s"}`, "round_robin")))

	if len(clientOptions) > 0 {
		client.AddOption(clientOptions...)
	}

	// var reg *prometheus.Registry
	// var metric *grpcprometheus.ServerMetrics
	if prometheusConfig.Enable {
		// cusMetrics := prommetrics.GetGrpcCusMetrics(rpcRegisterName, share)
		// reg, metric, _ = prommetrics.NewGrpcPromObj(cusMetrics)
		// options = append(options, mw.GrpcServer(), grpc.StreamInterceptor(metric.StreamServerInterceptor()),
		//	grpc.UnaryInterceptor(metric.UnaryServerInterceptor()))
		options = append(
			options, mw.GrpcServer(),
			prommetricsUnaryInterceptor(rpcRegisterName),
			prommetricsStreamInterceptor(rpcRegisterName),
		)

		var (
			listener net.Listener
		)

		if autoSetPorts {
			listener, prometheusPort, err = getAutoPort()
			if err != nil {
				return err
			}

			etcdClient := client.(*etcd.SvcDiscoveryRegistryImpl).GetClient()

			_, err = etcdClient.Put(ctx, prommetrics.BuildDiscoveryKey(rpcRegisterName), jsonutil.StructToJsonString(prommetrics.BuildDefaultTarget(registerIP, prometheusPort)))
			if err != nil {
				return errs.WrapMsg(err, "etcd put err")
			}
		} else {
			prometheusPort, err = datautil.GetElemByIndex(prometheusConfig.Ports, index)
			if err != nil {
				return err
			}
			listener, err = net.Listen("tcp", fmt.Sprintf(":%d", prometheusPort))
			if err != nil {
				return errs.WrapMsg(err, "listen err", "rpcTcpAddr", rpcTcpAddr)
			}
		}
		cs := prommetrics.GetGrpcCusMetrics(rpcRegisterName, share)
		go func() {
			if err := prommetrics.RpcInit(cs, listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				netErr = errs.WrapMsg(err, fmt.Sprintf("rpc %s prometheus start err: %d", rpcRegisterName, prometheusPort))
				netDone <- struct{}{}
			}
			//metric.InitializeMetrics(srv)
			// Create a HTTP server for prometheus.
			// httpServer = &http.Server{Handler: promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), Addr: fmt.Sprintf("0.0.0.0:%d", prometheusPort)}
			// if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			//	netErr = errs.WrapMsg(err, "prometheus start err", httpServer.Addr)
			//	netDone <- struct{}{}
			// }
		}()
	} else {
		options = append(options, mw.GrpcServer())
	}

	listener, port, err := getAutoPort()
	if err != nil {
		return err
	}

	log.CInfo(ctx, "RPC server is initializing", "rpcRegisterName", rpcRegisterName, "rpcPort", port,
		"prometheusPort", prometheusPort)

	defer listener.Close()
	srv := grpc.NewServer(options...)

	err = rpcFn(ctx, config, client, srv)
	if err != nil {
		return err
	}

	err = client.Register(
		ctx,
		rpcRegisterName,
		registerIP,
		port,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}

	go func() {
		err := srv.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			netErr = errs.WrapMsg(err, "rpc start err: ", rpcTcpAddr)
			netDone <- struct{}{}
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	select {
	case <-sigs:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := gracefulStopWithCtx(ctx, srv.GracefulStop); err != nil {
			return err
		}
		return nil
	case <-netDone:
		return netErr
	}
}

func gracefulStopWithCtx(ctx context.Context, f func()) error {
	done := make(chan struct{}, 1)
	go func() {
		f()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return errs.New("timeout, ctx graceful stop")
	case <-done:
		return nil
	}
}

func prommetricsUnaryInterceptor(rpcRegisterName string) grpc.ServerOption {
	getCode := func(err error) int {
		if err == nil {
			return 0
		}
		rpcErr, ok := err.(interface{ GRPCStatus() *status.Status })
		if !ok {
			return -1
		}
		return int(rpcErr.GRPCStatus().Code())
	}
	return grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		prommetrics.RPCCall(rpcRegisterName, info.FullMethod, getCode(err))
		return resp, err
	})
}

func prommetricsStreamInterceptor(rpcRegisterName string) grpc.ServerOption {
	return grpc.ChainStreamInterceptor()
}
