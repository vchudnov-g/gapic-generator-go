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

func getHTTPAnnotation(m *descriptor.MethodDescriptorProto) (allPatterns []string, method string, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("http annotation: %s", err)
		}
	}()

	eHTTP, err := proto.GetExtension(m.GetOptions(), annotations.E_Http)
	if m == nil || m.GetOptions() == nil || err == proto.ErrMissingExtension {
		return nil, "", nil
	} else if err != nil {
		return nil, "", err
	}

	http := eHTTP.(*annotations.HttpRule)
	rules := []*annotations.HttpRule{http}
	rules = append(rules, http.GetAdditionalBindings()...)
	allPatterns = []string{}

	for _, rule := range rules {
		pattern := ""

		switch rule.GetPattern().(type) {
		case *annotations.HttpRule_Get:
			pattern = rule.GetGet()
			method = "GET"
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
		default:
			return nil, "", fmt.Errorf("unhandled http method %#v", rule)
		}
		allPatterns = append(allPatterns, pattern)
	}
	return allPatterns, method, nil
}

var pathVariableRegexp = regexp.MustCompile("{([^}]+)}")

func restifyRequest(pt *printer.P, m *descriptor.MethodDescriptorProto) (method string, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("generating REST request code: %s", err)
		}
	}()

	p := pt.Printf

	allPatterns, method, err := getHTTPAnnotation(m)
	if err != nil || len(allPatterns) == 0 {
		return "", err
	}

	for _, onePattern := range allPatterns {
		p("// %q -> %q\n", *m.Name, onePattern)
	}

	pattern := allPatterns[0]
	err = restifyRequestPath(pt, m, pattern)

	// TODO(vchudnov): restifyRequestQueryParams
	// TODO(vchudnov): restifyRequestBody

	return method, err
}

func restifyRequestPath(pt *printer.P, m *descriptor.MethodDescriptorProto, pattern string) (err error) {
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
		terminator := ""
		if end < maxPatternIndex {
			terminator = ","
		}
		parts = append(parts, fmt.Sprintf("%q, %s%s", pattern[currentIdx:start], variable, terminator))
		currentIdx = end
	}
	if currentIdx < maxPatternIndex {
		parts = append(parts, fmt.Sprintf("%q", pattern[currentIdx:]))
	}
	p("  urlPath := fmt.Sprintf(%s)", strings.Join(parts, " "))
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

	// TODO(vchudnov) Implement resource name pattern
	// checking. Generate a function that keeps a map of all
	// regexes indexed by variableParts, and then matches the
	// field value against that.
	if len(valuePattern) > 0 {
		return "", fmt.Errorf("resource name pattern checking not implemented yet: %q", valuePattern)
	}

	if len(fieldName) == 0 {
		return "", fmt.Errorf("no field name provided")
	}
	return "req" + buildAccessor(fieldName), nil
}
