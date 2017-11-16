package main

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/Knetic/govaluate"
	"github.com/joyent/triton-go/compute"
)

var UserFunctions = map[string]govaluate.ExpressionFunction{
	"args": func(args ...interface{}) (interface{}, error) {
		for i, a := range args {
			fmt.Printf("arg[%d] = %v[%T]", i, a, a)
		}
		return nil, nil
	},

	"contains": func(args ...interface{}) (interface{}, error) {
		/*
					 Usage: contains(tags, "role", "zookeeper")  => contains(MAP, STRING(role), STRING(zookeeper)
			                 Usage: contains(network, "1234-321")        => contains(STRING(NETWORK[0]), STRING(NETWORK[1]), ..., "1234-321")
		*/
		if len(args) < 2 {
			return struct{}{}, fmt.Errorf("wrong number of argument(s) on contains(LIST, OBJECT)")
		}

		target := args[len(args)-1]

		value := reflect.ValueOf(args[0])

		if value.Kind() == reflect.Map {
			if len(args) == 2 {
				m, ok := args[0].(map[string]interface{})
				if !ok {
					return struct{}{}, fmt.Errorf("maps with non-string key are not supported: %T(%v)", args[0], args[0])
				}
				k, ok := args[1].(string)
				if !ok {
					return struct{}{}, fmt.Errorf("non-string keys are not supported: %T(%v)", args[1], args[1])
				}
				_, ok = m[k]
				// v, ok := m[k]
				//fmt.Printf("MAP existence m[%v], value = %v\n", m, v)
				return ok, nil
			} else {
				if (len(args)-1)%2 != 0 {
					return struct{}{}, fmt.Errorf("wrong number of argument(s) on contains(MAP, KEY, VALUE, ...)")
				}
				m, ok := args[0].(map[string]interface{})
				if !ok {
					return struct{}{}, fmt.Errorf("maps with non-string key are not supported: %T(%v)", args[0], args[0])
				}

				for i := 1; i < len(args); i += 2 {
					k, ok := args[i].(string)
					if !ok {
						return struct{}{}, fmt.Errorf("non-string keys are not supported: %T(%v)", args[i], args[i])
					}

					v := args[i+1]

					if m[k] != v {
						return false, nil
					}
				}
				return true, nil
			}
		}

		for _, a := range args[:len(args)-1] {
			//fmt.Printf("arg[%d]%T = %v\n", i, a, a)
			switch s := a.(type) {
			case string:
				if t, ok := target.(string); !ok {
					return struct{}{}, fmt.Errorf("cannot convert %v to a string", target)
				} else {
					if s == t {
						return true, nil
					}
				}
			case int:
				if t, ok := target.(int); !ok {
					return struct{}{}, fmt.Errorf("cannot convert %v to an int", target)
				} else {
					if s == t {
						return true, nil
					}
				}
			default:
				return struct{}{}, fmt.Errorf("type %T(%v) is not supported", s, s)
			}
		}
		return false, nil
	},
}

func buildContext(instance *compute.Instance, image *compute.Image) map[string]interface{} {
	context := make(map[string]interface{})

	// I wanted to explosing the genuien name as the output of Trion Cloud API, not the field name in Golang struct.
	// which may not possible with GOVALUATE.
	context["id"] = instance.ID
	context["name"] = instance.Name
	context["type"] = instance.Type
	context["brand"] = instance.Brand
	context["state"] = instance.State
	context["memory"] = instance.Memory
	context["disk"] = instance.Disk
	context["created"] = instance.Created
	context["updated"] = instance.Updated
	context["docker"] = instance.Docker
	context["ips"] = instance.IPs
	context["primaryIp"] = instance.PrimaryIP
	context["firewall_enabled"] = instance.FirewallEnabled
	context["compute_node"] = instance.ComputeNode
	context["package"] = instance.Package

	// parameters below here are subjected to change
	context["tags"] = instance.Tags
	context["image"] = instance.Image

	context["image_id"] = image.ID
	context["image_name"] = image.Name
	context["image_version"] = image.Version
	context["image_os"] = image.OS
	context["image_type"] = image.Type
	context["image_public"] = image.Public
	context["image_state"] = image.State
	context["image_tags"] = image.Tags
	context["image_owner"] = image.Owner
	context["image_published_at"] = image.PublishedAt

	context["networks"] = instance.Networks
	context["has_public_net"] = NetCache.HasPublic(instance)

	return context
}

func Evaluate(instance *compute.Instance, image *compute.Image, expression string) (bool, error) {
	context := buildContext(instance, image)

	{
		tokens := strings.Fields(expression)

		if len(tokens) == 1 && tokens[0] != "true" && tokens[0] != "false" && tokens[0][0] != '"' {
			expression = fmt.Sprintf("name == \"%s\"", tokens[0])
		}
	}

	ev, err := govaluate.NewEvaluableExpressionWithFunctions(expression, UserFunctions)

	if err != nil {
		return false, fmt.Errorf("parse error: %s\n", err)
	}

	result, err := ev.Evaluate(context)
	if err != nil {
		return false, fmt.Errorf("evaulate error: %s", err)
	}

	Debug.Printf("EVAL RESULT: %v", result)

	if r, ok := result.(bool); !ok {
		return false, fmt.Errorf("not boolean value: %v", result)
	} else {
		return r, nil
	}
}
