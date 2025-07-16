package generator

import (
	"fmt"
	"unicode"

	_ "embed"

	"github.com/a-h/templ/parser/v2"
)

func (g *generator) writeElementComponent(indentLevel int, n *parser.Element) (err error) {
	if len(n.Children) == 0 {
		return g.writeSelfClosingElementComponent(indentLevel, n)
	}
	return g.writeBlockElementComponent(indentLevel, n)
}

func (g *generator) writeSelfClosingElementComponent(indentLevel int, n *parser.Element) (err error) {
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

func (g *generator) writeBlockElementComponent(indentLevel int, n *parser.Element) (err error) {
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
	restAttrs []parser.Attribute
}

func (g *generator) writeElementComponentAttrVars(indentLevel int, n *parser.Element) ([]string, error) {
	// For now, we'll return empty arguments
	// In a full implementation, this would process attributes and create variables for them
	return []string{}, nil
}

func (g *generator) reorderElementComponentAttributes(n *parser.Element) (elementComponentAttributes, error) {
	// For now, return empty attributes
	// In a full implementation, this would analyze the component signature
	// and match attributes to parameters
	return elementComponentAttributes{
		keys:      []parser.ConstantAttributeKey{},
		attrs:     []parser.Attribute{},
		restAttrs: n.Attributes, // All attributes go to rest for now
	}, nil
}

func (g *generator) writeElementComponentAttrComponent(indentLevel int, attr parser.Attribute) (varName string, err error) {
	// For now, return empty string
	// In a full implementation, this would handle component-type attributes
	return "", nil
}

func (g *generator) writeElementComponentArgNewVar(indentLevel int, attr parser.Attribute) (string, error) {
	// For now, return empty string
	// In a full implementation, this would create variables for attribute values
	return "", nil
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

func (g *generator) writeElementComponentFunctionCall(indentLevel int, n *parser.Element) (err error) {
	// For now, we'll use a simple approach that generates function calls for all components
	// In the future, we can enhance this to handle struct types and methods
	
	if _, err = g.w.WriteIndent(indentLevel, `templ_7745c5c3_Err = `); err != nil {
		return err
	}
	
	var r parser.Range
	
	// Write the component name
	if r, err = g.w.Write(n.Name); err != nil {
		return err
	}
	g.sourceMap.Add(parser.Expression{Value: n.Name, Range: n.NameRange}, r)
	
	// Write the arguments
	if _, err = g.w.Write("("); err != nil {
		return err
	}
	
	// For now, we'll pass empty arguments
	// TODO: Process attributes and map them to function parameters
	
	if _, err = g.w.Write(")"); err != nil {
		return err
	}
	
	return nil
}

// isValidIdentifier checks if a string is a valid Go identifier
func isValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return false
			}
		}
	}
	return true
}
