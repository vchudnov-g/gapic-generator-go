// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gengapic

import (
	"fmt"
	"strings"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	conf "github.com/googleapis/gapic-generator-go/internal/grpc_service_config"
	"github.com/googleapis/gapic-generator-go/internal/pbinfo"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/genproto/googleapis/rpc/code"
)

// httpClientGenerator implements clientGenerator for the HTTP-transport case
type httpClientGenerator struct {
	g *generator
}

func (hcg *httpClientGenerator) clientHook(servName string) {
	g := hcg.g
	p := g.printf

	p("var new%sHTTPClientHook clientHook", servName)
	p("")
}

func (hcg *httpClientGenerator) clientOptions(serv *descriptor.ServiceDescriptorProto, servName string) error {
	g := hcg.g
	p := g.printf
	relyOnGrpcConfig := true

	// CallOptions struct
	if !relyOnGrpcConfig {
		p("// %[1]sCallOptions contains the retry settings for each method of %[1]sClient.", servName)
		p("type %sCallOptions struct {", servName)
		for _, m := range serv.Method {
			p("%s []gax.CallOption", *m.Name)
		}
		p("}")
		p("")

		g.imports[pbinfo.ImportSpec{"gax", "github.com/googleapis/gax-go/v2"}] = true
	}

	// defaultHTTPClientOptions
	{
		var host string
		if eHost, err := proto.GetExtension(serv.Options, annotations.E_DefaultHost); err == nil {
			host = *eHost.(*string)
		} else {
			fqn := g.descInfo.ParentFile[serv].GetPackage() + "." + serv.GetName()
			return fmt.Errorf("service %q is missing option google.api.default_host", fqn)
		}

		if !strings.Contains(host, ":") {
			host += ":443"
		}

		p("func default%sHTTPClientOptions() []option.ClientOption {", servName)
		p("  // TODO(vchudnov): Generate options for HTTP transport")
		p("  return []option.ClientOption{")
		p("    option.WithEndpoint(%q),", host)
		p("    option.WithGRPCDialOption(grpc.WithDisableServiceConfig()),")
		p("    option.WithScopes(DefaultAuthScopes()...),")
		p("    option.WithGRPCDialOption(grpc.WithDefaultCallOptions(")
		p("      grpc.MaxCallRecvMsgSize(math.MaxInt32))),")
		p("  }")
		p("}")
		p("")

		g.imports[pbinfo.ImportSpec{Path: "math"}] = true
		g.imports[pbinfo.ImportSpec{Path: "google.golang.org/api/option"}] = true
	}

	// defaultCallOptions
	if !relyOnGrpcConfig {
		sFQN := fmt.Sprintf("%s.%s", g.descInfo.ParentFile[serv].GetPackage(), serv.GetName())
		policies := map[string]*conf.MethodConfig_RetryPolicy{}
		reqLimits := map[string]int{}
		resLimits := map[string]int{}

		var methCfgs []*conf.MethodConfig
		if g.grpcConf != nil {
			methCfgs = g.grpcConf.GetMethodConfig()
		}

		// gather retry policies from MethodConfigs
		for _, mc := range methCfgs {
			for _, name := range mc.GetName() {
				base := name.GetService()

				// skip the Name entry if it's not the current service
				if base != sFQN {
					continue
				}

				// individual method config, overwrites service-level config
				if name.GetMethod() != "" {
					base = base + "." + name.GetMethod()
					policies[base] = mc.GetRetryPolicy()

					if maxReq := mc.GetMaxRequestMessageBytes(); maxReq != nil {
						reqLimits[base] = int(maxReq.GetValue())
					}

					if maxRes := mc.GetMaxResponseMessageBytes(); maxRes != nil {
						resLimits[base] = int(maxRes.GetValue())
					}

					continue
				}

				// service-level config, apply to all *unset* methods
				for _, m := range serv.GetMethod() {
					// build fully-qualified name
					fqn := base + "." + m.GetName()

					// set retry config
					if _, ok := policies[fqn]; !ok {
						policies[fqn] = mc.GetRetryPolicy()
					}

					// set max request size limit
					if maxReq := mc.GetMaxRequestMessageBytes(); maxReq != nil {
						if _, ok := reqLimits[fqn]; !ok {
							reqLimits[fqn] = int(maxReq.GetValue())
						}
					}

					// set max response size limit
					if maxRes := mc.GetMaxResponseMessageBytes(); maxRes != nil {
						if _, ok := resLimits[fqn]; !ok {
							resLimits[fqn] = int(maxRes.GetValue())
						}
					}
				}
			}
		}

		// read retry params from gRPC ServiceConfig
		p("func default%[1]sCallOptions() *%[1]sCallOptions {", servName)
		p("  return &%sCallOptions{", servName)
		for _, m := range serv.GetMethod() {
			mFQN := sFQN + "." + m.GetName()
			p("%s: []gax.CallOption{", m.GetName())

			if maxReq, ok := reqLimits[mFQN]; ok {
				p("gax.WithGRPCOptions(grpc.MaxCallSendMsgSize(%d)),", maxReq)
			}

			if maxRes, ok := resLimits[mFQN]; ok {
				p("gax.WithGRPCOptions(grpc.MaxCallRecvMsgSize(%d)),", maxRes)
			}

			if rp, ok := policies[mFQN]; ok && rp != nil {
				p("gax.WithRetry(func() gax.Retryer {")
				p("  return gax.OnCodes([]codes.Code{")
				for _, c := range rp.GetRetryableStatusCodes() {
					cstr := c.String()

					// Go uses the American-English spelling with a single "L"
					if c == code.Code_CANCELLED {
						cstr = "Canceled"
					}

					p("    codes.%s,", snakeToCamel(cstr))
				}
				p("	 }, gax.Backoff{")
				// this ignores max_attempts
				p("		Initial:    %d * time.Millisecond,", durationToMillis(rp.GetInitialBackoff()))
				p("		Max:        %d * time.Millisecond,", durationToMillis(rp.GetMaxBackoff()))
				p("		Multiplier: %.2f,", rp.GetBackoffMultiplier())
				p("	 })")
				p("}),")

				// include imports necessary for retry configuration
				g.imports[pbinfo.ImportSpec{Path: "time"}] = true
				g.imports[pbinfo.ImportSpec{Path: "google.golang.org/grpc/codes"}] = true
			}
			p("},")
		}
		p("  }")
		p("}")
		p("")
	}

	return nil
}

func (hcg *httpClientGenerator) clientInit(serv *descriptor.ServiceDescriptorProto, servName string) error {
	g := hcg.g
	p := g.printf

	var hasLRO bool
	for _, m := range serv.Method {
		if g.isLRO(m) {
			hasLRO = true
			break
		}
	}

	// imp, err := g.descInfo.ImportSpec(serv)
	// if err != nil {
	// 	return err
	// }

	// client struct
	{
		p("// %sHTTPClient is a client for interacting with %s.", servName, g.apiName)
		p("//")
		p("// Methods, except Close, may be called concurrently. However, fields must not be modified concurrently with method calls.")
		p("type %sHTTPClient struct {", servName)

		// p("// Connection pool of gRPC connections to the service.")
		// p("connPool gtransport.ConnPool")
		// p("")

		// p("// The gRPC API client.")
		// p("%s %s.%sClient", grpcClientField(servName), imp.Name, serv.GetName())
		// p("")

		if hasLRO {
			p("// LROClient is used internally to handle longrunning operations.")
			p("// It is exposed so that its CallOptions can be modified if required.")
			p("// Users should not Close this client.")
			p("LROClient *lroauto.OperationsClient")
			p("")

			g.imports[pbinfo.ImportSpec{Name: "lroauto", Path: "cloud.google.com/go/longrunning/autogen"}] = true
		}

		p("// The call options for this service.")
		p("CallOptions *%sCallOptions", servName)
		p("")

		p("// The x-goog-* metadata to be sent with each request.")
		p("// TOODO(vchudnov) Modify so this doesn't require grpc packages")
		p("xGoogMetadata metadata.MD")
		p("}")
		p("")

		g.imports[pbinfo.ImportSpec{Path: "google.golang.org/grpc"}] = true
		g.imports[pbinfo.ImportSpec{Path: "google.golang.org/grpc/metadata"}] = true
	}

	// Client constructor
	{
		clientName := camelToSnake(serv.GetName())
		clientName = strings.Replace(clientName, "_", " ", -1)

		p("// New%sClient creates a new %s client.", servName, clientName)
		p("//")
		g.comment(g.comments[serv])
		p("func New%[1]sHTTPClient(ctx context.Context, opts ...option.ClientOption) (*%[1]sHTTPClient, error) {", servName)
		p("  clientOpts := default%sHTTPClientOptions()", servName)
		p("")
		p("  if new%sClientHook != nil {", servName)
		p("    hookOpts, err := new%sHTTPClientHook(ctx, clientHookParams{})", servName)
		p("    if err != nil {")
		p("      return nil, err")
		p("    }")
		p("    clientOpts = append(clientOpts, hookOpts...)")
		p("  }")
		p("")
		p("  c := &%sHTTPClient{", servName)
		p("    CallOptions: default%sCallOptions(),", servName)
		p("")
		p("  }")
		p("  c.setGoogleClientInfo()")
		p("")

		if hasLRO {
			p("  c.LROClient, err = lroauto.NewOperationsClient(ctx, gtransport.WithConnPool(connPool))")
			p("  if err != nil {")
			p("    // This error \"should not happen\", since we are just reusing old connection pool")
			p("    // and never actually need to dial.")
			p("    // If this does happen, we could leak connp. However, we cannot close conn:")
			p("    // If the user invoked the constructor with option.WithGRPCConn,")
			p("    // we would close a connection that's still in use.")
			p("    // TODO: investigate error conditions.")
			p("    return nil, err")
			p("  }")
		}

		p("  return c, nil")
		p("}")
		p("")

		g.imports[pbinfo.ImportSpec{Name: "gtransport", Path: "google.golang.org/api/transport/grpc"}] = true
		g.imports[pbinfo.ImportSpec{Path: "context"}] = true
	}

	// // Close()
	// {
	// 	p("// Close closes the connection to the API service. The user should invoke this when")
	// 	p("// the client is no longer required.")
	// 	p("func (c *%sClient) Close() error {", servName)
	// 	p("  return c.connPool.Close()")
	// 	p("}")
	// 	p("")
	// }

	// setGoogleClientInfo
	{
		p("// setGoogleClientInfo sets the name and version of the application in")
		p("// the `x-goog-api-client` header passed on each request. Intended for")
		p("// use by Google-written clients.")
		p("func (c *%sHTTPClient) setGoogleClientInfo(keyval ...string) {", servName)
		p(`  kv := append([]string{"gl-go", versionGo()}, keyval...)`)
		p(`  kv = append(kv, "gapic", versionClient, "gax", gax.Version, "grpc", grpc.Version)`)
		p(`  c.xGoogMetadata = metadata.Pairs("x-goog-api-client", gax.XGoogHeader(kv...))`)
		p("}")
		p("")
	}

	return nil
}

// TODO(vchudnov). Remove; this is temporary. This should be
// part of generator, not clientGenerator. Using it here while
// we migrate the functions alled by generator.genMethod over
// to clientGenerator.
func (hcg *httpClientGenerator) genMethod(servName string, serv *descriptor.ServiceDescriptorProto, m *descriptor.MethodDescriptorProto) error {
	g := hcg.g
	p := g.printf
	p("// HTTP method %q goes here", *m.Name)
	p("")
	p("")
	return nil
}
