package main

import (
	"encoding/json"
	"fmt"
	"reflect"

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

func Evaluate(instance *compute.Instance, expression string) (bool, error) {
	b, err := json.Marshal(*instance)

	if err != nil {
		return false, fmt.Errorf("failure on unmarshalling Instance type: %s", err)
	}

	var context map[string]interface{}
	json.Unmarshal(b, &context)

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
