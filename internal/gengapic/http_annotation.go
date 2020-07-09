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
	"regexp"
	"strings"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/googleapis/gapic-generator-go/internal/printer"
	"google.golang.org/genproto/googleapis/api/annotations"
)

func getHTTPAnnotation(m *descriptor.MethodDescriptorProto) (allPatterns []string, method string, body string, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("http annotation: %s", err)
		}
	}()

	eHTTP, err := proto.GetExtension(m.GetOptions(), annotations.E_Http)
	if m == nil || m.GetOptions() == nil || err == proto.ErrMissingExtension {
		return nil, "", "", nil
	} else if err != nil {
		return nil, "", "", err
	}

	http := eHTTP.(*annotations.HttpRule)
	rules := []*annotations.HttpRule{http}
	rules = append(rules, http.GetAdditionalBindings()...)
	allPatterns = []string{}

	for _, rule := range rules {
		pattern := ""

		// Check as per AIP 127
		newBody := rule.GetBody()
		if len(body) > 0 && newBody != body {
			err = fmt.Errorf("inconsistent body annotations %q and %q", body, newBody)
		}
		body = newBody

		switch rule.GetPattern().(type) {
		case *annotations.HttpRule_Get:
			pattern = rule.GetGet()
			method = "GET"
			// Check as per AIP 127
			if len(body) > 0 {
				err = fmt.Errorf("unexpected body definition for GET method: %q", body)
			}
		case *annotations.HttpRule_Post:
			pattern = rule.GetPost()
			method = "POST"
		case *annotations.HttpRule_Patch:
			pattern = rule.GetPatch()
			method = "PATCH"
		case *annotations.HttpRule_Put:
			pattern = rule.GetPut()
			method = "PUT"
		case *annotations.HttpRule_Delete:
			pattern = rule.GetDelete()
			method = "DELETE"
			// Check as per AIP 127
			if len(body) > 0 {
				err = fmt.Errorf("unexpected body definition for DELETE method: %q", body)
			}
		default:
			err = fmt.Errorf("unhandled http method %#v", rule)
		}
		if err != nil {
			return nil, "", "", err
		}

		allPatterns = append(allPatterns, pattern)
	}
	return allPatterns, method, body, nil
}

var pathVariableRegexp = regexp.MustCompile("{([^}]+)}")

func restifyRequest(pt *printer.P, m *descriptor.MethodDescriptorProto, pathVariable, bodyVariable string) (method string, haveBody bool, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("generating REST request code: %s", err)
		}
	}()

	p := pt.Printf

	allPatterns, method, body, err := getHTTPAnnotation(m)
	if err != nil || len(allPatterns) == 0 {
		return "", false, err
	}

	for _, onePattern := range allPatterns {
		p("// %q -> %q\n", *m.Name, onePattern)
	}

	pattern := allPatterns[0]
	err = restifyRequestPath(pt, m, pathVariable, pattern)
	haveBody, err = restifyRequestBody(pt, m, bodyVariable, body)
	if err != nil {
		return "", false, err
	}

	// TODO(vchudnov): restifyRequestQueryParams

	return method, haveBody, err
}

func restifyRequestBody(pt *printer.P, m *descriptor.MethodDescriptorProto, bodyVariable string, body string) (haveBody bool, err error) {
	if len(body) == 0 {
		return false, nil
	}

	var restBody string
	if body == "*" {
		restBody = "req"
	} else {
		// TODO(vchudnov): Check the accessor below is valid.
		restBody = "req" + buildAccessor(body)
	}
	pt.Printf("  %s := %s", bodyVariable, restBody)
	return true, nil

}

func restifyRequestPath(pt *printer.P, m *descriptor.MethodDescriptorProto, pathVariable string, pattern string) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("in pattern %q: %s", pattern, err)
		}
	}()

	p := pt.Printf

	maxPatternIndex := len(pattern) - 1
	variableMatches := pathVariableRegexp.FindAllStringSubmatchIndex(pattern, -1) // {([^{}]+)}

	currentIdx := 0
	parts := []string{}
	for _, match := range variableMatches {
		start, end := match[0], match[1]
		variable, err := getVariableFor(pattern[start+1:end-1], m)
		if err != nil {
			return err
		}
		parts = append(parts, fmt.Sprintf("%q", pattern[currentIdx:start]), variable)
		currentIdx = end
	}
	if currentIdx < maxPatternIndex {
		parts = append(parts, fmt.Sprintf("%q", pattern[currentIdx:]))
	}
	p("  %s := fmt.Sprint(%s)", pathVariable, strings.Join(parts, ", "))

	return nil
}

func getVariableFor(variable string, m *descriptor.MethodDescriptorProto) (accessor string, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("in variable %q: %s", variable, err)
		}
	}()

	variableParts := strings.Split(variable, "=")
	var fieldName, valuePattern string
	if len(variableParts) > 2 {
		return "", fmt.Errorf(`multiple "="`)
	}

	fieldName = variableParts[0]
	if len(variableParts) > 1 {
		valuePattern = variableParts[1]
	}

	// TODO(vchudnov): Return a URL path fragment corresponding to
	// `valuePattern`. Right now we are not checking
	// `valuePattern`, but according to
	// https://google.aip.dev/127, this MUST be
	// specified. Currently, the Discovery-to-proto converter does
	// not provide a `valuePattern`, and this works fine with the PHP
	// HTTP transport in the monolith.
	//
	// TODO(vchudnov): As part of the above, check that we extract
	// the full resource name when appropriate (can we tell when
	// it's appropriate?). cf https://google.aip.dev/127
	if len(valuePattern) > 0 {
		return "", fmt.Errorf("resource name pattern checking not implemented yet: %q", valuePattern)
	}

	if len(fieldName) == 0 {
		return "", fmt.Errorf("no field name provided")
	}

	// TODO(vchudnov): Check the accessor below is valid.
	return "req" + buildAccessor(fieldName), nil
}
