package admin

import (
	"strings"
	"testing"
)

func TestManagedRoutesUIContract(t *testing.T) {
	for _, marker := range []string{
		"switchTab('managedRoutes',this)",
		"id=\"tabManagedRoutes\"",
		"/api/admin/managed-routes",
		"function loadManagedRoutes(",
		"function saveManagedRoute(",
		"function deleteManagedRoute(",
		"function addManagedRouteLine(",
		"credentials: 'same-origin'",
	} {
		if !strings.Contains(indexHTML, marker) {
			t.Fatalf("managed route UI marker %q is missing", marker)
		}
	}
	for _, forbidden := range []string{
		"strong-admin-token",
		"Authorization: Bearer",
		"Set-Cookie",
	} {
		if strings.Contains(indexHTML, forbidden) {
			t.Fatalf("managed route UI embeds sensitive marker %q", forbidden)
		}
	}
}

func TestProxyNodesUIUsesSingleFormAndHealthGatedSwitch(t *testing.T) {
	for _, marker := range []string{
		"id=\"proxyNodeModal\"",
		"id=\"proxyNodeForm\"",
		"function submitProxyNode(event)",
		"function switchProxyNode(id)",
		"创建并生成安装命令",
		"切换到此节点",
		"n.playback_healthy",
		"n.ingress_healthy",
		"n.last_heartbeat_at",
		"/api/admin/public-ingress/switch",
		"id=\"publicIngressSchedulerStatus\"",
		"/api/admin/public-ingress/status",
		"no_eligible_public_ingress_candidate",
		"window.publicIngressOperation = ingress.ok ? ingress.operation : null",
		"verifiedNodeId = window.publicIngressOperation?.phase === 'verified'",
		"n.id===verifiedNodeId",
		"\u6700\u9ad8\u4f18\u5148\u7ea7",
		"function copyProxyEnrollmentCommand()",
		"复制安装命令",
		"id=\"proxyNodeFilter\"",
		"异常（degraded）",
		"已停用/撤销",
		"['degraded','online'].includes(n.state)",
		"id=\"decommissionModal\"",
		"彻底移除",
		"function retryDecommissionJob(",
		"remote_cleanup_pending",
	} {
		if !strings.Contains(indexHTML, marker) {
			t.Fatalf("proxy node UI marker %q is missing", marker)
		}
	}
	if strings.Contains(indexHTML, "active: all.filter(n=>n.state==='healthy'&&n.enabled&&Number(n.priority||0)===1)") {
		t.Fatal("priority one must not be presented as the verified public ingress")
	}
	if strings.Contains(indexHTML, "window.prompt('节点名称") || strings.Contains(indexHTML, "window.prompt('Edge HTTPS ingress") {
		t.Fatal("proxy node creation must not use a prompt wizard")
	}
}
