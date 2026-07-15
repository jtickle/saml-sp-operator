package render

import "encoding/xml"

// attributemap.go renders attribute-map.xml (RENDER-03) — the SAML
// attribute id -> exported id mapping shibd decodes and the FastCGI
// authorizer then re-exports as an HTTP response header named
// "Variable-<id>" (attribute-map.xml lines 18-19 at repo root).
//
// Golden-fixture scope (same locked decision as shibboleth2.go /
// RESEARCH.md Pitfall 5): the byte-compare target is
// testdata/golden/attribute-map.xml, this package's own comment-free,
// machine-formatted semantic-tree reproduction of the repo-root
// attribute-map.xml — NOT that file's hand-authored prose comment block or
// its hand-aligned attribute columns (`name="email"     id="email"`), which
// encoding/xml cannot and should not reproduce.
//
// Every AttributeMapping string routed into this tree flows through a
// normal attr struct-tag field, which encoding/xml auto-escapes by
// construction (T-04-01 mitigation) — no manual escaping needed here.
//
// attribute-map.xml is reloadChanges="false" in shibboleth2.xml
// (shibAttributeMapPath), so its bytes must always be included in
// Hash's input set (D-08, T-04-02) — that wiring is the caller's
// responsibility (Phase 2), not this file's.

// attributeXML is one <Attribute name=... id=.../> child: name is the SAML
// attribute id as decoded off the assertion (AttributeMapping.Name), id is
// the internal id shibd re-exports as the "Variable-<id>" header
// (AttributeMapping.ExportedID).
type attributeXML struct {
	Name string `xml:"name,attr"`
	ID   string `xml:"id,attr"`
}

// attributesXML is the attribute-map.xml root element. Per D-03, xmlns and
// xmlns:xsi are declared as plain string attributes on this root struct
// ONLY — the child attributeXML carries a bare local-name tag (empty
// Space), matching spConfigXML's convention in shibboleth2.go.
type attributesXML struct {
	XMLName    xml.Name       `xml:"Attributes"`
	Xmlns      string         `xml:"xmlns,attr"`
	XmlnsXsi   string         `xml:"xmlns:xsi,attr"`
	Attributes []attributeXML `xml:"Attribute"`
}

// buildAttributeMapTree assembles the attribute-map.xml struct tree from
// attrs, preserving input slice order (never ranging a Go map) so the
// rendered <Attribute> order always matches the caller's slice order.
func buildAttributeMapTree(attrs []AttributeMapping) attributesXML {
	tree := attributesXML{
		Xmlns:    "urn:mace:shibboleth:2.0:attribute-map",
		XmlnsXsi: "http://www.w3.org/2001/XMLSchema-instance",
	}
	for _, a := range attrs {
		tree.Attributes = append(tree.Attributes, attributeXML{
			Name: a.Name,
			ID:   a.ExportedID,
		})
	}
	return tree
}

// RenderAttributeMap renders the full attribute-map.xml document for attrs
// (RENDER-03). Output equals testdata/golden/attribute-map.xml byte-for-byte
// for the sample input exercised by TestRenderAttributeMap.
func RenderAttributeMap(attrs []AttributeMapping) ([]byte, error) {
	tree := buildAttributeMapTree(attrs)

	body, err := xml.MarshalIndent(tree, "", "    ")
	if err != nil {
		return nil, err
	}

	body = collapseEmptyElements(body)
	return withXMLProlog(body), nil
}
