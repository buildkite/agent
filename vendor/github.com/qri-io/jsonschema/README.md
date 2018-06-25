# jsonschema
[![Qri](https://img.shields.io/badge/made%20by-qri-magenta.svg?style=flat-square)](https://qri.io)
[![GoDoc](https://godoc.org/github.com/qri-io/jsonschema?status.svg)](http://godoc.org/github.com/qri-io/jsonschema)
[![License](https://img.shields.io/github/license/qri-io/jsonschema.svg?style=flat-square)](./LICENSE)
[![Codecov](https://img.shields.io/codecov/c/github/qri-io/jsonschema.svg?style=flat-square)](https://codecov.io/gh/qri-io/jsonschema)
[![CI](https://img.shields.io/circleci/project/github/qri-io/jsonschema.svg?style=flat-square)](https://circleci.com/gh/qri-io/jsonschema)
[![Go Report Card](https://goreportcard.com/badge/github.com/qri-io/jsonschema)](https://goreportcard.com/report/github.com/qri-io/jsonschema)

golang implementation of the [JSON Schema Specification](http://json-schema.org/), which lets you write JSON that validates some other json. Rad.

### Package Features

* Encode schemas back to JSON
* Supply Your own Custom Validators
* Uses Standard Go idioms

### Getting Involved

We would love involvement from more people! If you notice any errors or would
like to submit changes, please see our
[Contributing Guidelines](./.github/CONTRIBUTING.md).

### Developing

We've set up a separate document for [developer guidelines](https://github.com/qri-io/jsonschema/blob/master/DEVELOPERS.md)!

## Basic Usage

Here's a quick example pulled from the [godoc](https://godoc.org/github.com/qri-io/jsonschema):

```go
package main

import (
	"encoding/json"
	"fmt"

	"github.com/qri-io/jsonschema"
)

func main() {
	var schemaData = []byte(`{
      "title": "Person",
      "type": "object",
      "properties": {
          "firstName": {
              "type": "string"
          },
          "lastName": {
              "type": "string"
          },
          "age": {
              "description": "Age in years",
              "type": "integer",
              "minimum": 0
          },
          "friends": {
            "type" : "array",
            "items" : { "title" : "REFERENCE", "$ref" : "#" }
          }
      },
      "required": ["firstName", "lastName"]
    }`)

	rs := &jsonschema.RootSchema{}
	if err := json.Unmarshal(schemaData, rs); err != nil {
		panic("unmarshal schema: " + err.Error())
	}

	var valid = []byte(`{
    "firstName" : "George",
    "lastName" : "Michael"
    }`)

	if errors := rs.ValidateBytes(valid); len(errors) > 0 {
		panic(err)
	}

	var invalidPerson = []byte(`{
    "firstName" : "Prince"
    }`)
	if errors := rs.ValidateBytes(invalidPerson); len(errors) > 0 {
  	fmt.Println(errs[0].Error())
  }

	var invalidFriend = []byte(`{
    "firstName" : "Jay",
    "lastName" : "Z",
    "friends" : [{
      "firstName" : "Nas"
      }]
    }`)
	if errors := rs.ValidateBytes(invalidFriend); len(errors) > 0 {
  	fmt.Println(errors[0].Error())
  }
}
```

## Custom Validators

The [godoc](https://godoc.org/github.com/qri-io/jsonschema) gives an example of how to supply your own validators to extend the standard keywords supported by the spec.

It involves two steps that should happen _before_ allocating any RootSchema instances that use the validator:
1. create a custom type that implements the `Validator` interface
2. call RegisterValidator with the keyword you'd like to detect in JSON, and a `ValMaker` function.


```go
package main

import (
  "encoding/json"
  "fmt"
  "github.com/qri-io/jsonschema"
)

// your custom validator
type IsFoo bool

// newIsFoo is a jsonschama.ValMaker
func newIsFoo() jsonschema.Validator {
  return new(IsFoo)
}

// Validate implements jsonschema.Validator
func (f IsFoo) Validate(data interface{}) []jsonschema.ValError {
  if str, ok := data.(string); ok {
    if str != "foo" {
      return []jsonschema.ValError{
        {Message: fmt.Sprintf("'%s' is not foo. It should be foo. plz make '%s' == foo. plz", str, str)},
      }
    }
  }
  return nil
}

func main() {
  // register a custom validator by supplying a function
  // that creates new instances of your Validator.
  jsonschema.RegisterValidator("foo", newIsFoo)

  schBytes := []byte(`{ "foo": true }`)

  // parse a schema that uses your custom keyword
  rs := new(jsonschema.RootSchema)
  if err := json.Unmarshal(schBytes, rs); err != nil {
    // Real programs handle errors.
    panic(err)
  }

  // validate some JSON
  errors := rs.ValidateBytes([]byte(`"bar"`))

  // print le error
  fmt.Println(errs[0].Error())

  // Output: 'bar' is not foo. It should be foo. plz make 'bar' == foo. plz
}
```

