import copy
import unittest
import tempfile
from pathlib import Path

from validate_assets import ValidationError, load_capability_catalog, validate_gateway_catalog_tool_record, extract_gateway_tool_names_from_source, validate_gateway_catalog_source_drift


class CatalogScopesTest(unittest.TestCase):
    def test_only_permission_checked_global_template_reads_can_omit_scope(self):
        tools = {tool["name"]: tool for tool in load_capability_catalog()["tools"]}
        record = copy.deepcopy(tools["delivery.deployment_templates.list"])
        validate_gateway_catalog_tool_record(record, set(), set())
        for change in ({"name": "delivery.applications.detail"}, {"riskLevel": "mutate"}, {"permissionKeys": []}):
            with self.subTest(change=change), self.assertRaises(ValidationError):
                validate_gateway_catalog_tool_record({**record, **change}, set(), set())


class DraftProviderCatalogTest(unittest.TestCase):
    def test_registered_draft_provider_is_checked_without_accepting_unknown_advertisements(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "catalog.go"
            source.write_text("""var defaultToolCatalog = []domainaigateway.ToolCapability{
    {
        Name: "builtin.read",
    },
}
func defaultResources() []domainaigateway.ResourceCapability {
    return []domainaigateway.ResourceCapability{
\t}
}
func defaultPrompts() []domainaigateway.PromptCapability {
    return []domainaigateway.PromptCapability{
\t}
}
""")
            source.with_name("draft_provider.go").write_text("""tools := []domainaigateway.ToolCapability{
    {Name: "delivery.drafts.create"},
    {Name: "delivery.drafts.confirm"},
}
""")
            expected = {"builtin.read", "delivery.drafts.create", "delivery.drafts.confirm"}
            self.assertEqual(extract_gateway_tool_names_from_source(source), expected)
            catalog = {"tools": [{"name": name, "status": "stable-runtime"} for name in expected]}
            validate_gateway_catalog_source_drift(catalog, source)
            catalog["tools"].append({"name": "delivery.drafts.unknown", "status": "stable-runtime"})
            with self.assertRaisesRegex(ValidationError, "delivery.drafts.unknown"):
                validate_gateway_catalog_source_drift(catalog, source)


if __name__ == "__main__":
    unittest.main()
