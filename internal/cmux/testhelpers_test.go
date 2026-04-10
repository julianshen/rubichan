package cmux_test

import "encoding/json"

// unmarshalParams decodes a jsonrpcRequest's Params into dst.
func unmarshalParams(req jsonrpcRequest, dst any) error {
	return json.Unmarshal(req.Params, dst)
}
