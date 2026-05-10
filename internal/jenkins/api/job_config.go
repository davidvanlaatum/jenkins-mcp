package api

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/jenkins/urlx"
)

const redactedValue = "[REDACTED]"

type xmlNode struct {
	Name     string
	Attrs    []xml.Attr
	Text     string
	Children []*xmlNode
}

type redactionFrame struct {
	name      string
	sensitive bool
	urlLike   bool
}

func (a *API) GetJobConfig(ctx context.Context, job, mode string, maxBytes int64) (model.JobConfig, error) {
	detail, err := a.GetJob(ctx, job)
	if err != nil {
		return model.JobConfig{}, err
	}
	result := model.JobConfig{
		Job:              detail.Job,
		Mode:             normalizeJobConfigMode(mode),
		ConfigAccessible: false,
		Source:           "api/json",
		Summary:          fallbackJobConfigSummary(detail),
	}

	status, body, _, err := a.client.GetText(ctx, urlx.JobPath(job)+"/config.xml", nil)
	if err != nil {
		result.AccessError = err.Error()
		result.Warnings = append(result.Warnings, model.ConfigWarning{
			Code:    "config_request_failed",
			Message: "Unable to read Jenkins config.xml; returned fallback job metadata.",
			Detail:  err.Error(),
		})
		return result, nil
	}
	if status != http.StatusOK {
		result.AccessError = fmt.Sprintf("Jenkins returned HTTP %d", status)
		result.Warnings = append(result.Warnings, configStatusWarning(status, string(body)))
		return result, nil
	}

	redacted, redactionWarnings := redactXML(body)
	result.Warnings = append(result.Warnings, redactionWarnings...)
	root, parseErr := parseXML(redacted)
	if parseErr != nil {
		result.Warnings = append(result.Warnings, model.ConfigWarning{
			Code:    "config_parse_failed",
			Message: "Read config.xml but could not parse it into a structured summary.",
			Detail:  parseErr.Error(),
		})
	} else if root != nil {
		result.Summary = mergeJobConfigSummary(summarizeConfigXML(root), result.Summary)
	}

	result.ConfigAccessible = true
	result.Source = "config.xml"
	if result.Mode == "xml" || result.Mode == "both" {
		result.Warnings = append(result.Warnings, model.ConfigWarning{
			Code:    "xml_best_effort_redaction",
			Message: "XML mode returns best-effort redacted config.xml; review output carefully because Jenkins plugin configs can contain sensitive data in custom fields.",
		})
		result.Bytes = len(redacted)
		xmlBytes := redacted
		if maxBytes > 0 && int64(len(xmlBytes)) > maxBytes {
			xmlBytes = xmlBytes[:maxBytes]
			result.Truncated = true
			result.Warnings = append(result.Warnings, model.ConfigWarning{
				Code:    "xml_truncated",
				Message: "Redacted config.xml was truncated to the requested or configured byte limit.",
			})
		}
		result.XML = string(xmlBytes)
	}
	return result, nil
}

func normalizeJobConfigMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "summary":
		return "summary"
	case "xml":
		return "xml"
	case "both":
		return "both"
	default:
		return "summary"
	}
}

func fallbackJobConfigSummary(job model.JobDetail) model.JobConfigSummary {
	return model.JobConfigSummary{
		Kind:        kindFromClass(job.Class),
		Description: job.Description,
		Disabled:    job.Disabled,
		Buildable:   &job.Buildable,
		Parameters:  sanitizeParameterDefinitions(job.Parameters),
	}
}

func sanitizeParameterDefinitions(parameters []model.ParameterDefinition) []model.ParameterDefinition {
	if len(parameters) == 0 {
		return nil
	}
	out := make([]model.ParameterDefinition, 0, len(parameters))
	for _, parameter := range parameters {
		sanitized := parameter
		if isSensitiveParameterDefinition(sanitized) {
			if sanitized.Default != nil {
				sanitized.Default = redactedValue
			}
			sanitized.Choices = nil
		} else {
			if defaultString, ok := sanitized.Default.(string); ok {
				if sanitizedDefault, changed := sanitizeURLValue(defaultString); changed {
					sanitized.Default = sanitizedDefault
				}
			}
			for i, choice := range sanitized.Choices {
				if sanitizedChoice, changed := sanitizeURLValue(choice); changed {
					sanitized.Choices[i] = sanitizedChoice
				}
			}
		}
		out = append(out, sanitized)
	}
	return out
}

func isSensitiveParameterDefinition(parameter model.ParameterDefinition) bool {
	return isSensitiveName(parameter.Name) || isSensitiveName(parameter.Type)
}

func mergeJobConfigSummary(summary, fallback model.JobConfigSummary) model.JobConfigSummary {
	if summary.Kind == "" || summary.Kind == "unknown" {
		summary.Kind = fallback.Kind
	}
	if summary.Description == "" {
		summary.Description = fallback.Description
	}
	if summary.Disabled == nil {
		summary.Disabled = fallback.Disabled
	}
	if summary.Buildable == nil {
		summary.Buildable = fallback.Buildable
	}
	if len(summary.Parameters) == 0 {
		summary.Parameters = fallback.Parameters
	}
	return summary
}

func configStatusWarning(status int, body string) model.ConfigWarning {
	code := "config_unavailable"
	message := "Jenkins did not return config.xml; returned fallback job metadata."
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		code = "config_permission_denied"
		message = "Jenkins denied config.xml access; returned fallback job metadata from api/json."
	}
	if status == http.StatusNotFound {
		code = "config_not_found"
	}
	return model.ConfigWarning{Code: code, Message: message, Detail: configStatusWarningDetail(body)}
}

func configStatusWarningDetail(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	return fmt.Sprintf("Jenkins returned a non-OK config.xml response body (%d bytes); body omitted to avoid exposing sensitive configuration content.", len(body))
}

func redactXML(raw []byte) ([]byte, []model.ConfigWarning) {
	raw = normalizeXMLDeclaration(raw)
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var out bytes.Buffer
	encoder := xml.NewEncoder(&out)
	var warnings []model.ConfigWarning
	redactDepth := 0
	redacted := false
	var stack []redactionFrame

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, []model.ConfigWarning{{
				Code:    "xml_redaction_failed",
				Message: "Could not parse config.xml for redaction; raw XML was not returned.",
				Detail:  err.Error(),
			}}
		}
		switch token := token.(type) {
		case xml.StartElement:
			sensitive := isSensitiveName(token.Name.Local) || isHighRiskValueName(token.Name.Local)
			urlLike := isURLLikeName(token.Name.Local)
			if sensitive {
				redactDepth++
			}
			stack = append(stack, redactionFrame{name: token.Name.Local, sensitive: sensitive, urlLike: urlLike})
			for i := range token.Attr {
				if redactDepth > 0 || isSensitiveName(token.Attr[i].Name.Local) || isHighRiskValueName(token.Attr[i].Name.Local) {
					token.Attr[i].Value = redactedValue
					redacted = true
					continue
				}
				if isURLLikeName(token.Attr[i].Name.Local) {
					if sanitized, ok := sanitizeURLValue(token.Attr[i].Value); ok {
						token.Attr[i].Value = sanitized
						redacted = true
					}
				}
			}
			if err := encoder.EncodeToken(token); err != nil {
				return nil, []model.ConfigWarning{{Code: "xml_redaction_failed", Message: "Could not encode redacted config.xml.", Detail: err.Error()}}
			}
		case xml.EndElement:
			if err := encoder.EncodeToken(token); err != nil {
				return nil, []model.ConfigWarning{{Code: "xml_redaction_failed", Message: "Could not encode redacted config.xml.", Detail: err.Error()}}
			}
			if len(stack) > 0 {
				frame := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if frame.sensitive && redactDepth > 0 {
					redactDepth--
				}
			}
		case xml.CharData:
			text := strings.TrimSpace(string(token))
			if redactDepth > 0 && text != "" {
				token = xml.CharData(redactedValue)
				redacted = true
			} else if text != "" && len(stack) > 0 && stack[len(stack)-1].urlLike {
				if sanitized, ok := sanitizeURLValue(string(token)); ok {
					token = xml.CharData(sanitized)
					redacted = true
				}
			}
			if err := encoder.EncodeToken(token); err != nil {
				return nil, []model.ConfigWarning{{Code: "xml_redaction_failed", Message: "Could not encode redacted config.xml.", Detail: err.Error()}}
			}
		case xml.Comment:
			if redactDepth > 0 && strings.TrimSpace(string(token)) != "" {
				token = xml.Comment(redactedValue)
				redacted = true
			}
			if err := encoder.EncodeToken(token); err != nil {
				return nil, []model.ConfigWarning{{Code: "xml_redaction_failed", Message: "Could not encode redacted config.xml.", Detail: err.Error()}}
			}
		case xml.ProcInst:
			if redactDepth > 0 && strings.TrimSpace(string(token.Inst)) != "" {
				token.Inst = []byte(redactedValue)
				redacted = true
			}
			if err := encoder.EncodeToken(token); err != nil {
				return nil, []model.ConfigWarning{{Code: "xml_redaction_failed", Message: "Could not encode redacted config.xml.", Detail: err.Error()}}
			}
		case xml.Directive:
			if redactDepth > 0 && strings.TrimSpace(string(token)) != "" {
				token = xml.Directive(redactedValue)
				redacted = true
			}
			if err := encoder.EncodeToken(token); err != nil {
				return nil, []model.ConfigWarning{{Code: "xml_redaction_failed", Message: "Could not encode redacted config.xml.", Detail: err.Error()}}
			}
		default:
			if err := encoder.EncodeToken(token); err != nil {
				return nil, []model.ConfigWarning{{Code: "xml_redaction_failed", Message: "Could not encode redacted config.xml.", Detail: err.Error()}}
			}
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, []model.ConfigWarning{{Code: "xml_redaction_failed", Message: "Could not finalize redacted config.xml.", Detail: err.Error()}}
	}
	if redacted {
		warnings = append(warnings, model.ConfigWarning{
			Code:    "xml_redacted",
			Message: "Sensitive and high-risk config.xml fields such as credentials, tokens, passwords, secrets, scripts, commands, generic values, and URL credentials were redacted.",
		})
	}
	return out.Bytes(), warnings
}

func isSensitiveName(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "-", ""), "_", ""))
	for _, marker := range []string{"password", "passwd", "passphrase", "secret", "token", "apikey", "accesskey", "privatekey", "clientsecret", "credential"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func isHighRiskValueName(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "-", ""), "_", ""))
	switch normalized {
	case "script", "groovyscript", "command", "commands", "commandline", "propertiescontent", "value", "defaultvalue":
		return true
	default:
		return false
	}
}

func isURLLikeName(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "-", ""), "_", ""))
	return normalized == "remote" ||
		normalized == "servername" ||
		normalized == "repository" ||
		normalized == "repositoryname" ||
		normalized == "repo" ||
		strings.Contains(normalized, "url") ||
		strings.Contains(normalized, "uri")
}

func sanitizeURLValue(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return value, false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value, false
	}

	changed := false
	if parsed.User != nil {
		parsed.User = url.User(redactedValue)
		changed = true
	}

	query := parsed.Query()
	for key, values := range query {
		if !isSensitiveURLQueryName(key) {
			continue
		}
		for i := range values {
			values[i] = redactedValue
			changed = true
		}
		query[key] = values
	}
	if changed {
		parsed.RawQuery = query.Encode()
	}
	return parsed.String(), changed
}

func isSensitiveURLQueryName(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "-", ""), "_", ""))
	for _, marker := range []string{"password", "passwd", "secret", "token", "apikey", "privatekey", "clientsecret", "credentialsid", "accesskey", "signature", "sig", "auth"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func parseXML(raw []byte) (*xmlNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(normalizeXMLDeclaration(raw)))
	var stack []*xmlNode
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			node := &xmlNode{Name: token.Name.Local, Attrs: token.Attr}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 1 {
				return stack[0], nil
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				text := strings.TrimSpace(string(token))
				if text != "" {
					current := stack[len(stack)-1]
					if current.Text == "" {
						current.Text = text
					}
				}
			}
		}
	}
	return nil, nil
}

func normalizeXMLDeclaration(raw []byte) []byte {
	trimmed := bytes.TrimLeft(raw, "\xef\xbb\xbf \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("<?xml")) {
		return raw
	}
	end := bytes.Index(trimmed, []byte("?>"))
	if end < 0 {
		return raw
	}
	decl := trimmed[:end]
	replacements := [][]byte{
		[]byte(`version='1.1'`),
		[]byte(`version="1.1"`),
		[]byte(`version = '1.1'`),
		[]byte(`version = "1.1"`),
	}
	for _, old := range replacements {
		if bytes.Contains(decl, old) {
			offset := len(raw) - len(trimmed)
			normalizedDecl := bytes.Replace(trimmed[:end], old, []byte(`version="1.0"`), 1)
			normalized := make([]byte, 0, len(raw)-len(trimmed[:end])+len(normalizedDecl))
			normalized = append(normalized, raw[:offset]...)
			normalized = append(normalized, normalizedDecl...)
			normalized = append(normalized, trimmed[end:]...)
			return normalized
		}
	}
	return raw
}

func summarizeConfigXML(root *xmlNode) model.JobConfigSummary {
	summary := model.JobConfigSummary{
		RootElement: root.Name,
		RootClass:   attr(root, "class"),
		Plugin:      attr(root, "plugin"),
		Kind:        kindFromClass(root.Name + " " + attr(root, "class")),
		Description: firstText(root, "description"),
	}
	if disabled, ok := parseBool(firstText(root, "disabled")); ok {
		summary.Disabled = &disabled
	}
	summary.ScriptPath = firstText(root, "scriptPath")
	if definition := firstChild(root, "definition"); definition != nil {
		summary.DefinitionClass = attr(definition, "class")
	}
	if orphaned := firstChild(root, "orphanedItemStrategy"); orphaned != nil {
		summary.OrphanedItemStrategy = componentClass(orphaned)
	}
	summary.Sources = collectSources(root)
	summary.Traits = collectComponentsUnder(root, "traits")
	summary.Triggers = collectComponentsUnder(root, "triggers")
	summary.JobProperties = collectComponentsUnder(root, "properties")
	summary.ProjectFactories = collectComponentsUnder(root, "projectFactories")
	return summary
}

func collectSources(root *xmlNode) []model.ConfigSource {
	var out []model.ConfigSource
	walk(root, func(node *xmlNode) {
		switch {
		case node.Name == "source":
			out = append(out, configSourceFromNode(node, "branchSource"))
		case node.Name == "navigator" || strings.Contains(componentClass(node), "SCMNavigator"):
			out = append(out, configSourceFromNode(node, "navigator"))
		case node.Name == "scm":
			out = append(out, configSourceFromNode(node, "scm"))
		}
	})
	return uniqueSources(out)
}

func configSourceFromNode(node *xmlNode, kind string) model.ConfigSource {
	return model.ConfigSource{
		Kind:          kind,
		Class:         componentClass(node),
		Plugin:        attr(node, "plugin"),
		ID:            firstText(node, "id"),
		Remote:        sanitizeExtractedURL(firstTextAny(node, "remote", "url", "repositoryUrl")),
		CredentialsID: firstTextAny(node, "credentialsId", "credentialsID"),
		RepoOwner:     firstTextAny(node, "repoOwner", "repoOwnerName", "owner"),
		Repository:    sanitizeExtractedURL(firstTextAny(node, "repository", "repositoryName", "repo")),
		ServerURL:     sanitizeExtractedURL(firstTextAny(node, "serverUrl", "serverAPIUrl", "apiUri", "serverName")),
		Traits:        traitClasses(node),
	}
}

func sanitizeExtractedURL(value string) string {
	if sanitized, ok := sanitizeURLValue(value); ok {
		return sanitized
	}
	return value
}

func collectComponentsUnder(root *xmlNode, containerName string) []model.ConfigComponent {
	var out []model.ConfigComponent
	for _, container := range children(root, containerName) {
		for _, child := range container.Children {
			out = append(out, componentFromNode(child))
		}
	}
	return uniqueComponents(out)
}

func children(root *xmlNode, name string) []*xmlNode {
	var out []*xmlNode
	walk(root, func(node *xmlNode) {
		if node.Name == name {
			out = append(out, node)
		}
	})
	return out
}

func componentFromNode(node *xmlNode) model.ConfigComponent {
	return model.ConfigComponent{Name: node.Name, Class: componentClass(node), Plugin: attr(node, "plugin")}
}

func componentClass(node *xmlNode) string {
	if class := attr(node, "class"); class != "" {
		return class
	}
	return node.Name
}

func traitClasses(node *xmlNode) []string {
	var traits []string
	for _, traitsNode := range children(node, "traits") {
		for _, trait := range traitsNode.Children {
			if class := componentClass(trait); class != "" {
				traits = append(traits, class)
			}
		}
	}
	return uniqueStrings(traits)
}

func kindFromClass(class string) string {
	class = strings.ToLower(class)
	switch {
	case strings.Contains(class, "organizationfolder"):
		return "organizationFolder"
	case strings.Contains(class, "workflowmultibranchproject") || strings.Contains(class, "multibranch"):
		return "multibranchProject"
	case strings.Contains(class, "workflowjob") || strings.Contains(class, "flow-definition"):
		return "branchJob"
	case strings.Contains(class, "folder"):
		return "folder"
	case strings.Contains(class, "project") || strings.Contains(class, "freestyle"):
		return "freestyle"
	default:
		return "unknown"
	}
}

func firstChild(node *xmlNode, name string) *xmlNode {
	for _, child := range node.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}

func firstText(root *xmlNode, name string) string {
	var found string
	walk(root, func(node *xmlNode) {
		if found == "" && node.Name == name {
			found = node.Text
		}
	})
	return found
}

func firstTextAny(root *xmlNode, names ...string) string {
	for _, name := range names {
		if text := firstText(root, name); text != "" {
			return text
		}
	}
	return ""
}

func attr(node *xmlNode, name string) string {
	for _, attr := range node.Attrs {
		if attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}

func parseBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func walk(node *xmlNode, visit func(*xmlNode)) {
	if node == nil {
		return
	}
	visit(node)
	for _, child := range node.Children {
		walk(child, visit)
	}
}

func uniqueComponents(in []model.ConfigComponent) []model.ConfigComponent {
	seen := map[string]bool{}
	var out []model.ConfigComponent
	for _, item := range in {
		key := item.Name + "\x00" + item.Class + "\x00" + item.Plugin
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func uniqueSources(in []model.ConfigSource) []model.ConfigSource {
	seen := map[string]bool{}
	var out []model.ConfigSource
	for _, item := range in {
		key := item.Kind + "\x00" + item.Class + "\x00" + item.ID + "\x00" + item.Remote + "\x00" + item.RepoOwner + "\x00" + item.Repository
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range in {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
