// Copyright (c) 2018 Tigera, Inc. All rights reserved.

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

package checker

import (
	"testing"

	. "github.com/onsi/gomega"

	authz "github.com/envoyproxy/data-plane-api/api/auth"
	"github.com/projectcalico/app-policy/policystore"
	"github.com/projectcalico/app-policy/proto"
)

// ActionFromString should parse strings in case insensitive mode.
func TestActionFromString(t *testing.T) {
	RegisterTestingT(t)

	Expect(ActionFromString("allow")).To(Equal(ALLOW))
	Expect(ActionFromString("Allow")).To(Equal(ALLOW))
	Expect(ActionFromString("deny")).To(Equal(DENY))
	Expect(ActionFromString("Deny")).To(Equal(DENY))
	Expect(ActionFromString("pass")).To(Equal(PASS))
	Expect(ActionFromString("Pass")).To(Equal(PASS))
	Expect(ActionFromString("log")).To(Equal(LOG))
	Expect(ActionFromString("Log")).To(Equal(LOG))
	Expect(func() { ActionFromString("no_match") }).To(Panic())
}

// A policy with no rules does not match.
func TestCheckPolicyNoRules(t *testing.T) {
	RegisterTestingT(t)

	policy := &proto.Policy{}
	req := &authz.CheckRequest{}
	Expect(checkPolicy(policy, req)).To(Equal(NO_MATCH))
}

// If rules exist, but none match, we should get NO_MATCH
// Rules that do match should return their Action.
// Log rules should continue processing.
func TestCheckPolicyRules(t *testing.T) {
	RegisterTestingT(t)

	policy := &proto.Policy{InboundRules: []*proto.Rule{
		{
			Action: "log",
			Http: &proto.HTTPSelector{
				Methods: []string{"GET", "POST"},
			},
		},
		{
			Action: "allow",
			Http: &proto.HTTPSelector{
				Methods: []string{"POST"},
			},
		},
		{
			Action: "deny",
			Http: &proto.HTTPSelector{
				Methods: []string{"GET"},
			},
		},
	}}
	req := &authz.CheckRequest{Attributes: &authz.AttributeContext{
		Source: &authz.AttributeContext_Peer{
			Principal: "spiffe://cluster.local/ns/default/sa/steve",
		},
		Request: &authz.AttributeContext_Request{
			Http: &authz.AttributeContext_HTTPRequest{Method: "HEAD"},
		},
	}}
	Expect(checkPolicy(policy, req)).To(Equal(NO_MATCH))

	http := req.GetAttributes().GetRequest().GetHttp()
	http.Method = "POST"
	Expect(checkPolicy(policy, req)).To(Equal(ALLOW))

	http.Method = "GET"
	Expect(checkPolicy(policy, req)).To(Equal(DENY))
}

// CheckStore when the store has no endpoint should deny requests.
func TestCheckStoreNoEndpoint(t *testing.T) {
	RegisterTestingT(t)

	store := policystore.NewPolicyStore()
	req := &authz.CheckRequest{Attributes: &authz.AttributeContext{
		Source: &authz.AttributeContext_Peer{
			Principal: "spiffe://cluster.local/ns/default/sa/steve",
		},
		Request: &authz.AttributeContext_Request{
			Http: &authz.AttributeContext_HTTPRequest{Method: "HEAD"},
		},
	}}
	status := checkStore(store, req)
	Expect(status.Code).To(Equal(PERMISSION_DENIED))
}

// CheckStore with no Tiers and no Profiles on the endpoint should deny.
func TestCheckStoreNoTiers(t *testing.T) {
	RegisterTestingT(t)

	store := policystore.NewPolicyStore()
	store.Endpoint = &proto.WorkloadEndpoint{
		Tiers: []*proto.TierInfo{},
	}
	req := &authz.CheckRequest{Attributes: &authz.AttributeContext{
		Source: &authz.AttributeContext_Peer{
			Principal: "spiffe://cluster.local/ns/default/sa/steve",
		},
		Request: &authz.AttributeContext_Request{
			Http: &authz.AttributeContext_HTTPRequest{Method: "HEAD"},
		},
	}}
	status := checkStore(store, req)
	Expect(status.Code).To(Equal(PERMISSION_DENIED))
}

// If a Policy matches, the action on the matched rule is the result.
func TestCheckStorePolicyMatch(t *testing.T) {
	RegisterTestingT(t)

	store := policystore.NewPolicyStore()
	store.Endpoint = &proto.WorkloadEndpoint{
		Tiers: []*proto.TierInfo{
			{
				Name:            "tier1",
				IngressPolicies: []string{"policy1", "policy2"},
			},
		},
	}
	store.PolicyByID[proto.PolicyID{Tier: "tier1", Name: "policy1"}] = &proto.Policy{
		InboundRules: []*proto.Rule{
			{
				Action: "deny",
				Http:   &proto.HTTPSelector{Methods: []string{"HEAD"}},
			},
		},
	}
	store.PolicyByID[proto.PolicyID{Tier: "tier1", Name: "policy2"}] = &proto.Policy{
		InboundRules: []*proto.Rule{
			{
				Action: "allow",
				Http:   &proto.HTTPSelector{Methods: []string{"GET"}},
			},
		},
	}

	req := &authz.CheckRequest{Attributes: &authz.AttributeContext{
		Source: &authz.AttributeContext_Peer{
			Principal: "spiffe://cluster.local/ns/default/sa/steve",
		},
		Request: &authz.AttributeContext_Request{
			Http: &authz.AttributeContext_HTTPRequest{Method: "GET"},
		},
	}}

	status := checkStore(store, req)
	Expect(status.Code).To(Equal(OK))

	http := req.GetAttributes().GetRequest().GetHttp()
	http.Method = "HEAD"

	status = checkStore(store, req)
	Expect(status.Code).To(Equal(PERMISSION_DENIED))
}

// And endpoint with no Policies should evaluate Profiles.
func TestCheckStoreProfileOnly(t *testing.T) {
	RegisterTestingT(t)

	store := policystore.NewPolicyStore()
	store.Endpoint = &proto.WorkloadEndpoint{
		Tiers:      []*proto.TierInfo{{}},
		ProfileIds: []string{"profile1", "profile2"},
	}
	store.ProfileByID[proto.ProfileID{Name: "profile1"}] = &proto.Profile{
		InboundRules: []*proto.Rule{
			{
				Action: "Deny",
				Http:   &proto.HTTPSelector{Methods: []string{"HEAD"}},
			},
		},
	}
	store.ProfileByID[proto.ProfileID{Name: "profile2"}] = &proto.Profile{
		InboundRules: []*proto.Rule{
			{
				Action: "allow",
				Http:   &proto.HTTPSelector{Methods: []string{"GET"}},
			},
		},
	}

	req := &authz.CheckRequest{Attributes: &authz.AttributeContext{
		Source: &authz.AttributeContext_Peer{
			Principal: "spiffe://cluster.local/ns/default/sa/steve",
		},
		Request: &authz.AttributeContext_Request{
			Http: &authz.AttributeContext_HTTPRequest{Method: "GET"},
		},
	}}

	status := checkStore(store, req)
	Expect(status.Code).To(Equal(OK))

	http := req.GetAttributes().GetRequest().GetHttp()
	http.Method = "HEAD"

	status = checkStore(store, req)
	Expect(status.Code).To(Equal(PERMISSION_DENIED))
}

// Ensure policy action of "Pass" ends policy evaluation and moves to profiles.
func TestCheckStorePass(t *testing.T) {
	RegisterTestingT(t)

	store := policystore.NewPolicyStore()
	store.Endpoint = &proto.WorkloadEndpoint{
		Tiers: []*proto.TierInfo{{
			Name:            "tier1",
			IngressPolicies: []string{"policy1", "policy2"},
		}},
		ProfileIds: []string{"profile1"},
	}

	// Policy1 matches and has action PASS, which means policy2 is not evaluated.
	store.PolicyByID[proto.PolicyID{Tier: "tier1", Name: "policy1"}] = &proto.Policy{
		InboundRules: []*proto.Rule{
			{
				Action: "pass",
				Http:   &proto.HTTPSelector{Methods: []string{"GET"}},
			},
		},
	}
	store.PolicyByID[proto.PolicyID{Tier: "tier1", Name: "policy2"}] = &proto.Policy{
		InboundRules: []*proto.Rule{
			{
				Action: "deny",
				Http:   &proto.HTTPSelector{Methods: []string{"GET"}},
			},
		},
	}

	// Profile1 matches and allows the traffic.
	store.ProfileByID[proto.ProfileID{Name: "profile1"}] = &proto.Profile{
		InboundRules: []*proto.Rule{
			{
				Action: "allow",
				Http:   &proto.HTTPSelector{Methods: []string{"HEAD", "GET"}},
			},
		},
	}

	req := &authz.CheckRequest{Attributes: &authz.AttributeContext{
		Source: &authz.AttributeContext_Peer{
			Principal: "spiffe://cluster.local/ns/default/sa/steve",
		},
		Request: &authz.AttributeContext_Request{
			Http: &authz.AttributeContext_HTTPRequest{Method: "GET"},
		},
	}}

	status := checkStore(store, req)
	Expect(status.Code).To(Equal(OK))
}