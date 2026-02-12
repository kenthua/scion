// Copyright 2026 Google LLC
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

package entadapter

import (
	"context"

	"github.com/ptone/scion-agent/pkg/ent"
	"github.com/ptone/scion-agent/pkg/store"
)

// CompositeStore wraps an existing store.Store and overrides group and policy
// operations with Ent-backed implementations.
type CompositeStore struct {
	store.Store
	groups   *GroupStore
	policies *PolicyStore
	client   *ent.Client
}

// NewCompositeStore creates a CompositeStore that delegates group and policy
// operations to Ent-backed stores while forwarding all other operations to the
// underlying store.
func NewCompositeStore(base store.Store, client *ent.Client) *CompositeStore {
	return &CompositeStore{
		Store:    base,
		groups:   NewGroupStore(client),
		policies: NewPolicyStore(client),
		client:   client,
	}
}

// Close closes both the Ent client and the underlying store.
func (c *CompositeStore) Close() error {
	if err := c.client.Close(); err != nil {
		_ = c.Store.Close()
		return err
	}
	return c.Store.Close()
}

// GroupStore method overrides — delegate to Ent-backed GroupStore.

func (c *CompositeStore) CreateGroup(ctx context.Context, group *store.Group) error {
	return c.groups.CreateGroup(ctx, group)
}

func (c *CompositeStore) GetGroup(ctx context.Context, id string) (*store.Group, error) {
	return c.groups.GetGroup(ctx, id)
}

func (c *CompositeStore) GetGroupBySlug(ctx context.Context, slug string) (*store.Group, error) {
	return c.groups.GetGroupBySlug(ctx, slug)
}

func (c *CompositeStore) UpdateGroup(ctx context.Context, group *store.Group) error {
	return c.groups.UpdateGroup(ctx, group)
}

func (c *CompositeStore) DeleteGroup(ctx context.Context, id string) error {
	return c.groups.DeleteGroup(ctx, id)
}

func (c *CompositeStore) ListGroups(ctx context.Context, filter store.GroupFilter, opts store.ListOptions) (*store.ListResult[store.Group], error) {
	return c.groups.ListGroups(ctx, filter, opts)
}

func (c *CompositeStore) AddGroupMember(ctx context.Context, member *store.GroupMember) error {
	return c.groups.AddGroupMember(ctx, member)
}

func (c *CompositeStore) RemoveGroupMember(ctx context.Context, groupID, memberType, memberID string) error {
	return c.groups.RemoveGroupMember(ctx, groupID, memberType, memberID)
}

func (c *CompositeStore) GetGroupMembers(ctx context.Context, groupID string) ([]store.GroupMember, error) {
	return c.groups.GetGroupMembers(ctx, groupID)
}

func (c *CompositeStore) GetUserGroups(ctx context.Context, userID string) ([]store.GroupMember, error) {
	return c.groups.GetUserGroups(ctx, userID)
}

func (c *CompositeStore) GetGroupMembership(ctx context.Context, groupID, memberType, memberID string) (*store.GroupMember, error) {
	return c.groups.GetGroupMembership(ctx, groupID, memberType, memberID)
}

func (c *CompositeStore) WouldCreateCycle(ctx context.Context, groupID, memberGroupID string) (bool, error) {
	return c.groups.WouldCreateCycle(ctx, groupID, memberGroupID)
}

func (c *CompositeStore) GetEffectiveGroups(ctx context.Context, userID string) ([]string, error) {
	return c.groups.GetEffectiveGroups(ctx, userID)
}

func (c *CompositeStore) GetGroupByGroveID(ctx context.Context, groveID string) (*store.Group, error) {
	return c.groups.GetGroupByGroveID(ctx, groveID)
}

func (c *CompositeStore) GetEffectiveGroupsForAgent(ctx context.Context, agentID string) ([]string, error) {
	return c.groups.GetEffectiveGroupsForAgent(ctx, agentID)
}

func (c *CompositeStore) CheckDelegatedAccess(ctx context.Context, agentID string, conditions *store.PolicyConditions) (bool, error) {
	return c.groups.CheckDelegatedAccess(ctx, agentID, conditions)
}

func (c *CompositeStore) GetGroupsByIDs(ctx context.Context, ids []string) ([]store.Group, error) {
	return c.groups.GetGroupsByIDs(ctx, ids)
}

// PolicyStore method overrides — delegate to Ent-backed PolicyStore.

func (c *CompositeStore) CreatePolicy(ctx context.Context, policy *store.Policy) error {
	return c.policies.CreatePolicy(ctx, policy)
}

func (c *CompositeStore) GetPolicy(ctx context.Context, id string) (*store.Policy, error) {
	return c.policies.GetPolicy(ctx, id)
}

func (c *CompositeStore) UpdatePolicy(ctx context.Context, policy *store.Policy) error {
	return c.policies.UpdatePolicy(ctx, policy)
}

func (c *CompositeStore) DeletePolicy(ctx context.Context, id string) error {
	return c.policies.DeletePolicy(ctx, id)
}

func (c *CompositeStore) ListPolicies(ctx context.Context, filter store.PolicyFilter, opts store.ListOptions) (*store.ListResult[store.Policy], error) {
	return c.policies.ListPolicies(ctx, filter, opts)
}

func (c *CompositeStore) AddPolicyBinding(ctx context.Context, binding *store.PolicyBinding) error {
	return c.policies.AddPolicyBinding(ctx, binding)
}

func (c *CompositeStore) RemovePolicyBinding(ctx context.Context, policyID, principalType, principalID string) error {
	return c.policies.RemovePolicyBinding(ctx, policyID, principalType, principalID)
}

func (c *CompositeStore) GetPolicyBindings(ctx context.Context, policyID string) ([]store.PolicyBinding, error) {
	return c.policies.GetPolicyBindings(ctx, policyID)
}

func (c *CompositeStore) GetPoliciesForPrincipal(ctx context.Context, principalType, principalID string) ([]store.Policy, error) {
	return c.policies.GetPoliciesForPrincipal(ctx, principalType, principalID)
}

func (c *CompositeStore) GetPoliciesForPrincipals(ctx context.Context, principals []store.PrincipalRef) ([]store.Policy, error) {
	return c.policies.GetPoliciesForPrincipals(ctx, principals)
}
