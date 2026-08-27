// Package client is generated from oncall's docs/swagger.json. Do not hand-edit
// client.gen.go; re-run `go generate ./...` after updating swagger.json.
package client

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 --config=oapi-codegen.yaml swagger.json

// oapi-codegen v2 emits every `*JSONRequestBody` as a fresh type definition
// (`type XJSONRequestBody XJSONBody`) rather than an alias. For the union
// bodies swag produces (`oneOf: [{type: object}, $ref]`), `XJSONBody` carries
// a custom MarshalJSON that serializes the private `union` field; the type
// definition does NOT inherit it, so the generated request builders
// `json.Marshal` an empty `{}` and every create/update call sends a body with
// no fields (observed as HTTP 400 "name is required" / 403 on team-scoped
// creates). The `-alias-types` flag that used to prevent this was removed in
// oapi-codegen v2, so rewrite the declarations to aliases post-generation.
// NOTE: `go generate` expands `$name` sequences on the command line (an
// unknown `$1` becomes empty), so the perl capture-group refs are written
// with `$DOLLAR`, which `go generate` replaces with a literal `$`, yielding
// `${1}`/`${2}` for perl.
//go:generate perl -0pi -e "s/^type (\\w+JSONRequestBody) (\\w+JSONBody)$/type $DOLLAR{1} = $DOLLAR{2}/mg" client.gen.go

// oncall documents rotation_length as an ISO 8601 duration string
// (swaggertype:"string"), so oapi-codegen types it `*string`, but the API
// serializes responses as an object ({months,days,micros}) and `*string` then
// fails to unmarshal it. Retype the field to the hand-written *Interval
// (interval.go), which round-trips both wire forms.
//go:generate perl -pi -e "s/RotationLength \\*string /RotationLength *Interval /g" client.gen.go
//go:generate gofmt -w client.gen.go
