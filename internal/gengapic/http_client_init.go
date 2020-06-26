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

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/googleapis/gapic-generator-go/internal/pbinfo"
	"github.com/googleapis/gapic-generator-go/internal/printer"
)

// httpClientGenerator implements clientGenerator for the HTTP-transport case
type httpClientGenerator struct {
	g *generator
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
		p("type %sClient struct {", servName)
		p("// The HTTP API client.")
		p("  client *http.Client")
		p("")

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
		p("func New%[1]sClient(ctx context.Context, opts ...option.ClientOption) (*%[1]sClient, error) {", servName)
		p("  clientOpts := default%sClientOptions()", servName)
		p("")
		p("  if new%sClientHook != nil {", servName)
		p("    hookOpts, err := new%sClientHook(ctx, clientHookParams{})", servName)
		p("    if err != nil {")
		p("      return nil, err")
		p("    }")
		p("    clientOpts = append(clientOpts, hookOpts...)")
		p("  }")
		p("")
		p("  c := &%sClient{", servName)
		p("    client: &http.Client{},")
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
		p("func (c *%sClient) setGoogleClientInfo(keyval ...string) {", servName)
		p(`  kv := append([]string{"gl-go", versionGo()}, keyval...)`)
		p(`  kv = append(kv, "gapic", versionClient, "gax", gax.Version, "grpc", grpc.Version)`)
		p(`  c.xGoogMetadata = metadata.Pairs("x-goog-api-client", gax.XGoogHeader(kv...))`)
		p("}")
		p("")
	}

	return nil
}

func (hcg *httpClientGenerator) insertMetadata(m *descriptor.MethodDescriptorProto) error {
	// TODO(vchudnov) Stub for now
	g := hcg.g
	p := g.printf
	p("")
	p("  // TODO(vchudnov): Some metadata goes here")
	p("")
	return nil
}

func (hcg *httpClientGenerator) clientCall(pt *printer.P, servName string, m *descriptor.MethodDescriptorProto) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("generating call code for %q", *m.Name)
		}
	}()

	g := hcg.g
	p := g.printf

	method, err := restifyRequest(pt, m)
	if err != nil {
		return err
	}

	p("")
	p("httpReq, err := http.NewRequestWithContext(ctx, %q, urlPath, nil)", method)
	p("if err != nil {")
	p(`  return fmt.Errorf("creating http request %%q: %%s", urlPath, err)`)
	p("}")
	p("")
	p("httpResp, err := c.client.Do(httpReq)")
	p("if err != nil {")
	p(`  return fmt.Errorf("issuing http request %%q: %%s", urlPath, err)`)
	p("}")
	p("")
	p("// TODO(vchudnov): Placeholders during development")
	p("fmt.Printf(%q, httpResp)", "response: %#v")
	p("")

	g.imports[pbinfo.ImportSpec{Path: "net/http"}] = true

	// TODO(vchudnov) NEXT: generate HTTP request
	// TODO(vchudnov) add auth
	// TODO(vchudnov) parse response body

	return nil
}

func (hcg *httpClientGenerator) clientType() string {
	return "HTTP"
}
