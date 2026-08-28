package cli

import (
	"context"
	"strings"
	"testing"

	apppb "go.viam.com/api/app/v1"
	"go.viam.com/test"
	"google.golang.org/grpc"

	"go.viam.com/rdk/cli/module_generate/modulegen"
	"go.viam.com/rdk/testutils/inject"
)

func testOrgs() []*apppb.Organization {
	return []*apppb.Organization{
		{Id: "11111111-1111-1111-1111-111111111111", Name: "OTF", PublicNamespace: ""},
		{Id: "22222222-2222-2222-2222-222222222222", Name: "Acme", PublicNamespace: "acme"},
		{Id: "33333333-3333-3333-3333-333333333333", Name: "Widgets", PublicNamespace: "widgets"},
	}
}

func TestFindUserOrg(t *testing.T) {
	t.Parallel()
	orgs := testOrgs()

	t.Run("matches public namespace", func(t *testing.T) {
		t.Parallel()
		org, err := findUserOrg(orgs, "acme")
		test.That(t, err, test.ShouldBeNil)
		test.That(t, org.GetId(), test.ShouldEqual, "22222222-2222-2222-2222-222222222222")
	})

	t.Run("matches public namespace case-insensitively", func(t *testing.T) {
		t.Parallel()
		org, err := findUserOrg(orgs, "ACME")
		test.That(t, err, test.ShouldBeNil)
		test.That(t, org.GetName(), test.ShouldEqual, "Acme")
	})

	t.Run("matches org name when namespace is unset", func(t *testing.T) {
		t.Parallel()
		org, err := findUserOrg(orgs, "otf")
		test.That(t, err, test.ShouldBeNil)
		test.That(t, org.GetId(), test.ShouldEqual, "11111111-1111-1111-1111-111111111111")
		test.That(t, org.GetPublicNamespace(), test.ShouldEqual, "")
	})

	t.Run("matches org id", func(t *testing.T) {
		t.Parallel()
		org, err := findUserOrg(orgs, "33333333-3333-3333-3333-333333333333")
		test.That(t, err, test.ShouldBeNil)
		test.That(t, org.GetName(), test.ShouldEqual, "Widgets")
	})

	t.Run("unknown identifier lists orgs", func(t *testing.T) {
		t.Parallel()
		_, err := findUserOrg(orgs, "not-an-org")
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, `none of your organizations match "not-an-org"`)
		test.That(t, err.Error(), test.ShouldContainSubstring, "OTF")
		test.That(t, err.Error(), test.ShouldContainSubstring, "no public namespace")
		test.That(t, err.Error(), test.ShouldNotContainSubstring, "not a member")
	})

	t.Run("empty identifier", func(t *testing.T) {
		t.Parallel()
		_, err := findUserOrg(orgs, "  ")
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "must provide")
	})

	t.Run("duplicate names", func(t *testing.T) {
		t.Parallel()
		dupes := []*apppb.Organization{
			{Id: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Name: "dup", PublicNamespace: "one"},
			{Id: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Name: "dup", PublicNamespace: "two"},
		}
		_, err := findUserOrg(dupes, "dup")
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "multiple organizations named")
		test.That(t, err.Error(), test.ShouldContainSubstring, "namespace: one")
		test.That(t, err.Error(), test.ShouldContainSubstring, "namespace: two")
	})
}

func TestSanitizeAndSuggestNamespace(t *testing.T) {
	t.Parallel()
	test.That(t, sanitizeNamespace("OTF Robotics"), test.ShouldEqual, "otf-robotics")
	test.That(t, sanitizeNamespace("  Acme  "), test.ShouldEqual, "acme")
	test.That(t, isValidPublicNamespace("otf"), test.ShouldBeTrue)
	test.That(t, isValidPublicNamespace("OTF"), test.ShouldBeFalse)
	test.That(t, isValidPublicNamespace("1otf"), test.ShouldBeFalse)

	org := &apppb.Organization{Name: "OTF", PublicNamespace: ""}
	test.That(t, suggestedNamespace("otf", org), test.ShouldEqual, "otf")
	test.That(t, suggestedNamespace("11111111-1111-1111-1111-111111111111", org), test.ShouldEqual, "otf")
}

func TestWrapResolveOrg(t *testing.T) {
	origPersist := persistCLIConfig
	persistCLIConfig = func(*Config) error { return nil }
	t.Cleanup(func() { persistCLIConfig = origPersist })

	listOrgs := func(orgs []*apppb.Organization) *inject.AppServiceClient {
		return &inject.AppServiceClient{
			ListOrganizationsFunc: func(ctx context.Context, in *apppb.ListOrganizationsRequest,
				opts ...grpc.CallOption,
			) (*apppb.ListOrganizationsResponse, error) {
				return &apppb.ListOrganizationsResponse{Organizations: orgs}, nil
			},
		}
	}

	t.Run("matches org name and uses public namespace", func(t *testing.T) {
		cCtx, ac, _, _ := setup(listOrgs([]*apppb.Organization{
			{Id: "11111111-1111-1111-1111-111111111111", Name: "otf", PublicNamespace: "otf"},
		}), nil, nil, nil, "token")
		mod := &modulegen.ModuleInputs{Namespace: "OTF", RegisterOnApp: true}
		test.That(t, wrapResolveOrg(context.Background(), cCtx, ac, mod), test.ShouldBeNil)
		test.That(t, mod.OrgID, test.ShouldEqual, "11111111-1111-1111-1111-111111111111")
		test.That(t, mod.Namespace, test.ShouldEqual, "otf")
		test.That(t, ac.conf.RecentModuleNamespaces, test.ShouldResemble, []string{"otf"})
	})

	t.Run("org name without namespace is a clear error, not membership", func(t *testing.T) {
		cCtx, ac, _, _ := setup(listOrgs([]*apppb.Organization{
			{Id: "11111111-1111-1111-1111-111111111111", Name: "otf", PublicNamespace: ""},
		}), nil, nil, nil, "token")
		mod := &modulegen.ModuleInputs{Namespace: "otf", RegisterOnApp: true}
		err := wrapResolveOrg(context.Background(), cCtx, ac, mod)
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "no public namespace")
		test.That(t, err.Error(), test.ShouldContainSubstring, "Settings")
		test.That(t, err.Error(), test.ShouldNotContainSubstring, "not a member")
		test.That(t, len(ac.conf.RecentModuleNamespaces), test.ShouldEqual, 0)
	})

	t.Run("unknown identifier lists orgs and does not claim non-membership", func(t *testing.T) {
		cCtx, ac, _, _ := setup(listOrgs(testOrgs()), nil, nil, nil, "token")
		mod := &modulegen.ModuleInputs{Namespace: "nope", RegisterOnApp: true}
		err := wrapResolveOrg(context.Background(), cCtx, ac, mod)
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, `none of your organizations match "nope"`)
		test.That(t, err.Error(), test.ShouldContainSubstring, "Acme")
		test.That(t, err.Error(), test.ShouldNotContainSubstring, "not a member")
	})

	t.Run("skips cloud resolve when not registering", func(t *testing.T) {
		cCtx, ac, _, _ := setup(listOrgs(nil), nil, nil, nil, "token")
		mod := &modulegen.ModuleInputs{Namespace: "my-org", RegisterOnApp: false}
		test.That(t, wrapResolveOrg(context.Background(), cCtx, ac, mod), test.ShouldBeNil)
		test.That(t, mod.Namespace, test.ShouldEqual, "myorg")
		test.That(t, mod.OrgID, test.ShouldEqual, "myorg")
		test.That(t, ac.conf.RecentModuleNamespaces, test.ShouldResemble, []string{"my-org"})
	})
}

func TestRecentModuleNamespaces(t *testing.T) {
	t.Parallel()
	test.That(t, prependRecent([]string{"acme", "widgets"}, "otf", 5), test.ShouldResemble, []string{"otf", "acme", "widgets"})
	test.That(t, prependRecent([]string{"otf", "acme"}, "OTF", 5), test.ShouldResemble, []string{"OTF", "acme"})
	test.That(t, prependRecent([]string{"a", "b", "c", "d", "e"}, "f", 5), test.ShouldResemble, []string{"f", "a", "b", "c", "d"})
	test.That(t, recentNamespaceHint(nil), test.ShouldEqual, "")
	test.That(t, recentNamespaceHint([]string{"otf", "acme"}), test.ShouldEqual, "Recently used: otf, acme")
	test.That(t, dedupeKeepOrder([]string{"otf", "OTF", "acme", ""}), test.ShouldResemble, []string{"otf", "acme"})
}

func TestOrgChoiceLabel(t *testing.T) {
	t.Parallel()
	test.That(t, orgChoiceLabel(&apppb.Organization{Name: "Acme", PublicNamespace: "acme"}),
		test.ShouldEqual, "Acme  (namespace: acme)")
	test.That(t, orgChoiceLabel(&apppb.Organization{Name: "OTF"}),
		test.ShouldEqual, "OTF  (no public namespace)")
	test.That(t, strings.Contains(formatOrgList(testOrgs()), "widgets"), test.ShouldBeTrue)
}
