package generator

import (
	"fmt"
	"slices"

	_ "embed"

	"github.com/a-h/templ/parser/v2"
)

func (g *generator) writeElementComponent(indentLevel int, n *parser.ElementComponent) (err error) {
	if len(n.Children) == 0 {
		return g.writeSelfClosingElementComponent(indentLevel, n)
	}
	return g.writeBlockElementComponent(indentLevel, n)
}

func (g *generator) writeSelfClosingElementComponent(indentLevel int, n *parser.ElementComponent) (err error) {
	// templ_7745c5c3_Err = Component(arg1, arg2, ...)
	if err = g.writeElementComponentFunctionCall(indentLevel, n); err != nil {
		return err
	}
	// .Render(ctx, templ_7745c5c3_Buffer)
	if _, err = g.w.Write(".Render(ctx, templ_7745c5c3_Buffer)\n"); err != nil {
		return err
	}
	if err = g.writeErrorHandler(indentLevel); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeBlockElementComponent(indentLevel int, n *parser.ElementComponent) (err error) {
	childrenName := g.createVariableName()
	if _, err = g.w.WriteIndent(indentLevel, childrenName+" := templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {\n"); err != nil {
		return err
	}
	indentLevel++
	if _, err = g.w.WriteIndent(indentLevel, "templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context\n"); err != nil {
		return err
	}
	if err := g.writeTemplBuffer(indentLevel); err != nil {
		return err
	}
	// ctx = templ.InitializeContext(ctx)
	if _, err = g.w.WriteIndent(indentLevel, "ctx = templ.InitializeContext(ctx)\n"); err != nil {
		return err
	}
	if err = g.writeNodes(indentLevel, stripLeadingAndTrailingWhitespace(n.Children), nil); err != nil {
		return err
	}
	// return nil
	if _, err = g.w.WriteIndent(indentLevel, "return nil\n"); err != nil {
		return err
	}
	indentLevel--
	if _, err = g.w.WriteIndent(indentLevel, "})\n"); err != nil {
		return err
	}
	if err = g.writeElementComponentFunctionCall(indentLevel, n); err != nil {
		return err
	}

	// .Render(templ.WithChildren(ctx, children), templ_7745c5c3_Buffer)
	if _, err = g.w.Write(".Render(templ.WithChildren(ctx, " + childrenName + "), templ_7745c5c3_Buffer)\n"); err != nil {
		return err
	}
	if err = g.writeErrorHandler(indentLevel); err != nil {
		return err
	}
	return nil
}

type elementComponentAttributes struct {
	keys      []parser.ConstantAttributeKey
	attrs     []parser.Attribute
	params    []ParameterInfo
	restAttrs []parser.Attribute
	restParam ParameterInfo
}

func (g *generator) writeElementComponentAttrVars(indentLevel int, sigs ComponentSignature, n *parser.ElementComponent) ([]string, error) {
	orderedAttrs, err := g.reorderElementComponentAttributes(sigs, n)
	if err != nil {
		return nil, err
	}

	var restVarName string
	if orderedAttrs.restParam.Name != "" {
		restVarName = g.createVariableName()
		if _, err = g.w.WriteIndent(indentLevel, "var "+restVarName+" = templ.OrderedAttributes{}\n"); err != nil {
			return nil, err
		}
	}

	res := make([]string, len(orderedAttrs.attrs))
	for i, attr := range orderedAttrs.attrs {
		param := orderedAttrs.params[i]
		value, err := g.writeElementComponentArgNewVar(indentLevel, attr, param)
		if err != nil {
			return nil, err
		}
		res[i] = value
	}

	if orderedAttrs.restParam.Name != "" {
		// spew.Dump(orderedAttrs.restParam, orderedAttrs.restAttrs)
		for _, attr := range orderedAttrs.restAttrs {
			_ = g.writeElementComponentArgRestVar(indentLevel, restVarName, attr)
		}
		res = append(res, restVarName)
	}
	return res, nil
}

func (g *generator) reorderElementComponentAttributes(sig ComponentSignature, n *parser.ElementComponent) (elementComponentAttributes, error) {
	rest := make([]parser.Attribute, 0)
	attrMap := make(map[string]parser.Attribute)
	keyMap := make(map[string]parser.ConstantAttributeKey)
	for _, attr := range n.Attributes {
		keyed, ok := attr.(parser.KeyedAttribute)
		if ok {
			key, ok := keyed.AttributeKey().(parser.ConstantAttributeKey)
			if ok {
				if slices.ContainsFunc(sig.Parameters, func(p ParameterInfo) bool { return p.Name == key.Name }) {
					// Element component should only works with const key element.
					attrMap[key.Name] = attr
					keyMap[key.Name] = key
					continue
				}
			}
		}
		rest = append(rest, attr)
	}

	params := sig.Parameters
	// We support an optional last parameter that is of type templ.Attributer.
	var attrParam ParameterInfo
	if len(params) > 0 && params[len(params)-1].IsAttributer {
		attrParam = params[len(params)-1]
		params = params[:len(params)-1]
	}
	ordered := make([]parser.Attribute, len(params))
	keys := make([]parser.ConstantAttributeKey, len(params))
	for i, param := range params {
		var ok bool
		ordered[i], ok = attrMap[param.Name]
		if !ok {
			return elementComponentAttributes{}, fmt.Errorf("missing required attribute %s for component %s", param.Name, n.Name)
		}
		keys[i], ok = keyMap[param.Name]
		if !ok {
			return elementComponentAttributes{}, fmt.Errorf("missing required key for attribute %s in component %s", param.Name, n.Name)
		}
	}
	return elementComponentAttributes{
		params:    sig.Parameters,
		attrs:     ordered,
		keys:      keys,
		restAttrs: rest,
		restParam: attrParam,
	}, nil
}

func (g *generator) writeElementComponentAttrComponent(indentLevel int, attr parser.Attribute, param ParameterInfo) (varName string, err error) {
	switch attr := attr.(type) {
	case *parser.InlineComponentAttribute:
		return g.writeChildrenComponent(indentLevel, attr.Children)
	case *parser.ExpressionAttribute:
		varName = g.createVariableName()
		var r parser.Range
		if _, err = g.w.WriteIndent(indentLevel, varName+", templ_7745c5c3_Err := templ.JoinAnyErrs("); err != nil {
			return "", err
		}
		if r, err = g.w.Write(attr.Expression.Value); err != nil {
			return "", err
		}
		g.sourceMap.Add(attr.Expression, r)
		if _, err = g.w.Write(")\n"); err != nil {
			return "", err
		}
		if err = g.writeExpressionErrorHandler(indentLevel, attr.Expression); err != nil {
			return "", err
		}
		return fmt.Sprintf("templ.Stringable(%s)", varName), nil
	case *parser.ConstantAttribute:
		value := `"` + attr.Value + `"`
		if attr.SingleQuote {
			value = `'` + attr.Value + `'`
		}
		varName = g.createVariableName()
		if _, err = g.w.WriteIndent(indentLevel, varName+" := templ.Stringable("+value+")\n"); err != nil {
			return "", err
		}
		return varName, nil
	default:
		return "", fmt.Errorf("unsupported attribute type %T for templ.Component parameter", attr)
	}
}

func (g *generator) writeElementComponentArgNewVar(indentLevel int, attr parser.Attribute, param ParameterInfo) (string, error) {
	if param.IsComponent {
		return g.writeElementComponentAttrComponent(indentLevel, attr, param)
	}

	switch attr := attr.(type) {
	case *parser.ConstantAttribute:
		quote := `"`
		if attr.SingleQuote {
			quote = `'`
		}
		value := quote + attr.Value + quote
		return value, nil
	case *parser.ExpressionAttribute:
		// TODO: support URL, Script and Style attribute
		// check writeExpressionAttribute
		var r parser.Range
		var err error
		vn := g.createVariableName()
		// vn, templ_7745c5c3_Err := templ.JoinAnyErrs(
		if _, err = g.w.WriteIndent(indentLevel, vn+", templ_7745c5c3_Err := templ.JoinAnyErrs("); err != nil {
			return "", err
		}
		// p.Name()
		if r, err = g.w.Write(attr.Expression.Value); err != nil {
			return "", err
		}
		g.sourceMap.Add(attr.Expression, r)
		if _, err = g.w.Write(")\n"); err != nil {
			return "", err
		}
		// Error handler
		if err = g.writeExpressionErrorHandler(indentLevel, attr.Expression); err != nil {
			return "", err
		}
		return vn, nil
	case *parser.BoolConstantAttribute:
		return "true", nil
	case *parser.BoolExpressionAttribute:
		// For boolean expressions that might return errors, use JoinAnyErrs
		vn := g.createVariableName()
		var err error
		// vn, templ_7745c5c3_Err := templ.JoinAnyErrs(expression)
		if _, err = g.w.WriteIndent(indentLevel, vn+", templ_7745c5c3_Err := templ.JoinAnyErrs("); err != nil {
			return "", err
		}
		var r parser.Range
		if r, err = g.w.Write(attr.Expression.Value); err != nil {
			return "", err
		}
		g.sourceMap.Add(attr.Expression, r)
		if _, err = g.w.Write(")\n"); err != nil {
			return "", err
		}
		// Error handler
		if err = g.writeExpressionErrorHandler(indentLevel, attr.Expression); err != nil {
			return "", err
		}
		return vn, nil
	default:
		return "", fmt.Errorf("unsupported attribute type %T in Element component argument", attr)
	}
}

func (g *generator) writeElementComponentArgRestVar(indentLevel int, restVarName string, attr parser.Attribute) error {
	var err error
	switch attr := attr.(type) {
	case *parser.BoolConstantAttribute:
		if err = g.writeRestAppend(indentLevel, restVarName, attr.Key.String(), "true"); err != nil {
			return err
		}
	case *parser.ConstantAttribute:
		value := `"` + attr.Value + `"`
		if attr.SingleQuote {
			value = `'` + attr.Value + `'`
		}
		if err = g.writeRestAppend(indentLevel, restVarName, attr.Key.String(), value); err != nil {
			return err
		}
	case *parser.BoolExpressionAttribute:
		if _, err = g.w.WriteIndent(indentLevel, `if `); err != nil {
			return err
		}
		if _, err = g.w.Write(attr.Expression.Value); err != nil {
			return err
		}
		if _, err = g.w.Write(" {\n"); err != nil {
			return err
		}
		{
			indentLevel++
			if err := g.writeRestAppend(indentLevel, restVarName, attr.Key.String(), "true"); err != nil {
				return err
			}
			indentLevel--
		}
		if _, err = g.w.WriteIndent(indentLevel, "}\n"); err != nil {
			return err
		}
	case *parser.ExpressionAttribute:
		attrKey := attr.Key.String()
		if isScriptAttribute(attrKey) {
			vn := g.createVariableName()
			if _, err = g.w.WriteIndent(indentLevel, "var "+vn+" templ.ComponentScript = "); err != nil {
				return err
			}
			var r parser.Range
			if r, err = g.w.Write(attr.Expression.Value); err != nil {
				return err
			}
			g.sourceMap.Add(attr.Expression, r)
			if _, err = g.w.Write("\n"); err != nil {
				return err
			}
			if err := g.writeRestAppend(indentLevel, restVarName, attrKey, vn+".Call"); err != nil {
				return err
			}
		} else if attrKey == "style" {
			var r parser.Range
			vn := g.createVariableName()
			// var vn string
			if _, err = g.w.WriteIndent(indentLevel, "var "+vn+" string\n"); err != nil {
				return err
			}
			// vn, templ_7745c5c3_Err = templruntime.SanitizeStyleAttributeValues(
			if _, err = g.w.WriteIndent(indentLevel, vn+", templ_7745c5c3_Err = templruntime.SanitizeStyleAttributeValues("); err != nil {
				return err
			}
			// value
			if r, err = g.w.Write(attr.Expression.Value); err != nil {
				return err
			}
			g.sourceMap.Add(attr.Expression, r)
			// )
			if _, err = g.w.Write(")\n"); err != nil {
				return err
			}
			if err = g.writeErrorHandler(indentLevel); err != nil {
				return err
			}
			if err = g.writeRestAppend(indentLevel, restVarName, attrKey, vn); err != nil {
				return err
			}
		} else {
			vn := g.createVariableName()
			var r parser.Range
			if r, err = g.w.WriteIndent(indentLevel, fmt.Sprintf("%s, templ_7745c5c3_Err := templ.JoinAnyErrs(%s)\n", vn, attr.Expression.Value)); err != nil {
				return err
			}
			g.sourceMap.Add(attr.Expression, r)
			if err := g.writeErrorHandler(indentLevel); err != nil {
				return err
			}
			if err = g.writeRestAppend(indentLevel, restVarName, attr.Key.String(), vn); err != nil {
				return err
			}
		}
	case *parser.ConditionalAttribute:
		if _, err = g.w.WriteIndent(indentLevel, `if `); err != nil {
			return err
		}
		var r parser.Range
		if r, err = g.w.Write(attr.Expression.Value); err != nil {
			return err
		}
		g.sourceMap.Add(attr.Expression, r)
		if _, err = g.w.Write(" {\n"); err != nil {
			return err
		}
		{
			indentLevel++
			for _, attr := range attr.Then {
				if err := g.writeElementComponentArgRestVar(indentLevel, restVarName, attr); err != nil {
					return err
				}
			}
			indentLevel--
		}
		if len(attr.Else) > 0 {
			if _, err = g.w.WriteIndent(indentLevel, "} else {\n"); err != nil {
				return err
			}
			{
				indentLevel++
				for _, attr := range attr.Else {
					if err := g.writeElementComponentArgRestVar(indentLevel, restVarName, attr); err != nil {
						return err
					}
				}
			}
		}
		if _, err = g.w.WriteIndent(indentLevel, "}\n"); err != nil {
			return err
		}
	case *parser.SpreadAttributes:
		if _, err = g.w.WriteIndent(indentLevel, fmt.Sprintf("%s = append(%s, %s.Items()...)\n", restVarName, restVarName, attr.Expression.Value)); err != nil {
			return err
		}
	case *parser.AttributeComment:
		return nil
	default:
		return fmt.Errorf("TODO: support attribute type %T in Element component argument", attr)
	}
	return err
}

func (g *generator) writeRestAppend(indentLevel int, restVarName string, key string, val string) error {
	_, err := g.w.WriteIndent(indentLevel,
		fmt.Sprintf("%s = append(%s, templ.KeyValue[string, any]{Key: \"%s\", Value: %s})\n",
			restVarName, restVarName, key, val))
	return err
}

func (g *generator) writeElementComponentFunctionCall(indentLevel int, n *parser.ElementComponent) (err error) {
	// Try to resolve component on-demand
	sigs, err := g.resolveElementComponent(n)
	if err != nil {
		return fmt.Errorf("component %s at %s:%d:%d: %w", n.Name, g.options.FileName, n.Range.From.Line, n.Range.From.Col, err)
	}

	var vars []string
	if vars, err = g.writeElementComponentAttrVars(indentLevel, sigs, n); err != nil {
		return err
	}

	if _, err = g.w.WriteIndent(indentLevel, `templ_7745c5c3_Err = `); err != nil {
		return err
	}

	var r parser.Range

	// For types that implement Component, use appropriate struct literal syntax
	if sigs.IsStruct {
		// (ComponentType{Field1: value1, Field2: value2}) or (&ComponentType{...})
		if _, err = g.w.Write("("); err != nil {
			return err
		}

		if sigs.IsPointerRecv {
			if _, err = g.w.Write("&"); err != nil {
				return err
			}
		}
		if r, err = g.w.Write(n.Name); err != nil {
			return err
		}
		g.sourceMap.Add(parser.Expression{Value: n.Name, Range: n.NameRange}, r)

		if _, err = g.w.Write("{"); err != nil {
			return err
		}

		// Write field assignments for struct literal
		for i, arg := range vars {
			if i > 0 {
				if _, err = g.w.Write(", "); err != nil {
					return err
				}
			}
			// Write field name: value
			if i < len(sigs.Parameters) {
				if _, err = g.w.Write(sigs.Parameters[i].Name); err != nil {
					return err
				}
				if _, err = g.w.Write(": "); err != nil {
					return err
				}
			}
			if _, err = g.w.Write(arg); err != nil {
				return err
			}
		}

		if _, err = g.w.Write("})"); err != nil {
			return err
		}
	} else {
		// For functions, use function call syntax
		if r, err = g.w.Write(n.Name); err != nil {
			return err
		}
		g.sourceMap.Add(parser.Expression{Value: n.Name, Range: n.NameRange}, r)

		if _, err = g.w.Write("("); err != nil {
			return err
		}

		for i, arg := range vars {
			if i > 0 {
				if _, err = g.w.Write(", "); err != nil {
					return err
				}
			}
			r, err := g.w.Write(arg)
			if err != nil {
				return err
			}
			_ = r // TODO: Add source map for the key
		}

		if _, err = g.w.Write(")"); err != nil {
			return err
		}
	}

	return nil
}





// resolveElementComponent resolves a component on-demand during code generation
func (g *generator) resolveElementComponent(n *parser.ElementComponent) (ComponentSignature, error) {
	// Get current package path for local resolution
	currentPkgPath, err := g.getCurrentPackagePath()
	if err != nil {
		currentPkgPath = "" // Continue without current package path
	}
	
	// Use the symbol resolver's unified element component resolution
	return g.symbolResolver.ResolveElementComponent(g.currentFileDir(), currentPkgPath, n.Name, g.tf)
}

// collectAndResolveComponents is no longer needed - components are resolved on-demand
// This function is kept for backward compatibility but does nothing
func (g *generator) collectAndResolveComponents() error {
	return nil
}

// addComponentDiagnostic adds a diagnostic for component resolution issues
func (g *generator) addComponentDiagnostic(comp ComponentReference, message string) {
	// Create a Range from the component's position
	// ComponentReference.Position is the start position of the component name
	nameStart := comp.Position
	nameLength := int64(len(comp.Name))
	nameEnd := parser.Position{
		Index: nameStart.Index + nameLength,
		Line:  nameStart.Line,
		Col:   nameStart.Col + uint32(len(comp.Name)),
	}

	g.diagnostics = append(g.diagnostics, parser.Diagnostic{
		Message: message,
		Range: parser.Range{
			From: nameStart,
			To:   nameEnd,
		},
	})
}

// Type checking functions removed - now using cached type analysis in ParameterInfo
