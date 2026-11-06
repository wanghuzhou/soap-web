package soapuiproj

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"soapweb/internal/model"
)

// Marshal serialises a model.Project back into a SoapUI-compatible
// soapui-project XML document. The output is designed to round-trip through
// Parse (and to be openable by SoapUI itself for the parts we model:
// interface / operation / call / request, plus test suites).
func Marshal(p model.Project) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(xml.Header)

	projName := p.Name
	if projName == "" {
		projName = "soap-web-project"
	}

	// Root element with the con namespace.
	fmt.Fprintf(&b, `<con:soapui-project id="%s" name="%s" soapui-version="5.7.0" xmlns:con="http://eviware.com/soapui/config">`+"\n",
		xmlAttr(projectID(p)), xmlAttr(projName))
	b.WriteString("  <con:settings/>\n")

	for _, svc := range p.Services {
		writeInterface(&b, svc)
	}
	for _, ts := range p.TestSuites {
		writeTestSuite(&b, ts)
	}

	b.WriteString("  <con:properties/>\n")
	b.WriteString("</con:soapui-project>\n")
	return b.Bytes(), nil
}

func projectID(p model.Project) string {
	if p.ID != "" {
		return p.ID
	}
	return "soap-web-project-id"
}

func writeInterface(b *bytes.Buffer, svc model.Service) {
	fmt.Fprintf(b, `  <con:interface xsi:type="con:WsdlInterface" name="%s" type="wsdl" soapVersion="1_1" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`+"\n",
		xmlAttr(svc.Name))
	if svc.Wsdl != "" {
		fmt.Fprintf(b, "    <con:definitionCache type=\"TEXT\" rootPart=\"%s\"/>\n", xmlAttr(svc.Wsdl))
	} else {
		b.WriteString("    <con:definitionCache type=\"TEXT\"/>\n")
	}
	b.WriteString("    <con:endpoints>\n")
	seenEP := map[string]bool{}
	writeEP := func(ep string) {
		ep = strings.TrimSpace(ep)
		if ep == "" || seenEP[ep] {
			return
		}
		seenEP[ep] = true
		fmt.Fprintf(b, "      <con:endpoint>%s</con:endpoint>\n", xmlText(ep))
	}
	for _, op := range svc.Operations {
		writeEP(op.Endpoint)
		for _, c := range op.Calls {
			writeEP(c.Endpoint)
		}
	}
	b.WriteString("    </con:endpoints>\n")

	for _, op := range svc.Operations {
		fmt.Fprintf(b, `    <con:operation name="%s" action="%s" type="Request-Response">`+"\n",
			xmlAttr(op.Name), xmlAttr(op.SoapAction))
		b.WriteString("      <con:settings/>\n")
		for _, c := range op.Calls {
			writeCall(b, svc, op, c)
		}
		b.WriteString("    </con:operation>\n")
	}
	b.WriteString("  </con:interface>\n")
}

func writeCall(b *bytes.Buffer, svc model.Service, op model.Operation, c model.Call) {
	title := c.Title
	if title == "" {
		title = op.Name
	}
	fmt.Fprintf(b, `      <con:call id="%s" name="%s">`+"\n", xmlAttr(callID(c)), xmlAttr(title))
	b.WriteString("        <con:settings/>\n")
	ep := firstNonEmpty(c.Endpoint, op.Endpoint)
	if ep != "" {
		fmt.Fprintf(b, "        <con:endpoint>%s</con:endpoint>\n", xmlText(ep))
	}
	action := firstNonEmpty(c.Action, op.SoapAction)
	b.WriteString("        <con:request>\n")
	// SoapUI stores the request body as CDATA inside <con:request>. If the body
	// contains "]]>" we fall back to escaped text to keep the XML valid.
	body := c.Request
	if body == "" {
		// Fall back to the operation default request if the call is empty.
		body = op.Request
	}
	if strings.Contains(body, "]]>") {
		b.WriteString("          " + xmlText(body) + "\n")
	} else {
		b.WriteString("          <![CDATA[" + body + "]]>\n")
	}
	b.WriteString("        </con:request>\n")
	if action != "" {
		fmt.Fprintf(b, `        <con:wsaConfig action="%s"/>`+"\n", xmlAttr(action))
	}
	b.WriteString("      </con:call>\n")
}

func writeTestSuite(b *bytes.Buffer, ts model.TestSuite) {
	fmt.Fprintf(b, `  <con:testSuite id="%s" name="%s">`+"\n", xmlAttr(newIDIfEmpty(ts.Name)), xmlAttr(ts.Name))
	for _, tc := range ts.TestCases {
		fmt.Fprintf(b, `    <con:testCase name="%s">`+"\n", xmlAttr(tc.Name))
		for _, st := range tc.Steps {
			fmt.Fprintf(b, `      <con:testStep type="%s" name="%s">`+"\n", xmlAttr(st.Type), xmlAttr(st.Name))
			b.WriteString("        <con:config>\n")
			b.WriteString("          <con:request>\n")
			if st.Endpoint != "" {
				fmt.Fprintf(b, "            <con:endpoint>%s</con:endpoint>\n", xmlText(st.Endpoint))
			}
			body := st.Request
			if strings.Contains(body, "]]>") {
				b.WriteString("            " + xmlText(body) + "\n")
			} else {
				b.WriteString("            <![CDATA[" + body + "]]>\n")
			}
			b.WriteString("          </con:request>\n")
			b.WriteString("        </con:config>\n")
			b.WriteString("      </con:testStep>\n")
		}
		b.WriteString("    </con:testCase>\n")
	}
	b.WriteString("  </con:testSuite>\n")
}

func callID(c model.Call) string {
	if c.ID != "" {
		return c.ID
	}
	return "call-" + strings.ReplaceAll(c.Title, " ", "-")
}

func newIDIfEmpty(seed string) string {
	return "suite-" + strings.ReplaceAll(seed, " ", "-")
}

// xmlAttr escapes a string for use inside a double-quoted XML attribute.
func xmlAttr(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	out := buf.String()
	// EscapeText does not escape quotes; do it manually.
	out = strings.ReplaceAll(out, `"`, "&quot;")
	return out
}

// xmlText escapes a string for use as XML character data.
func xmlText(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}
