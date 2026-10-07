package dto

import (
	"encoding/json"
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// DecisionsRequest is shared by TypeSafe System One and OpenRouter Decisions.
// State and question instructions may contain arbitrary structured JSON.
type DecisionsRequest struct {
	Model     string          `json:"model"`
	State     json.RawMessage `json:"state"`
	Questions json.RawMessage `json:"questions"`
	Stream    *bool           `json:"stream,omitempty"`
	RawBody   json.RawMessage `json:"-"`
}

func (r *DecisionsRequest) UnmarshalJSON(data []byte) error {
	type plain DecisionsRequest
	var decoded plain
	if err := kitutil.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = DecisionsRequest(decoded)
	r.RawBody = append(json.RawMessage(nil), data...)
	return nil
}

// MarshalJSON preserves provider preferences and future fields, changing only
// the model after channel mapping. Parameter overrides operate on this body.
func (r DecisionsRequest) MarshalJSON() ([]byte, error) {
	if len(r.RawBody) == 0 {
		type plain DecisionsRequest
		return kitutil.Marshal(plain(r))
	}
	var fields map[string]json.RawMessage
	if err := kitutil.Unmarshal(r.RawBody, &fields); err != nil {
		return nil, err
	}
	model, err := kitutil.Marshal(r.Model)
	if err != nil {
		return nil, err
	}
	fields["model"] = model
	return kitutil.Marshal(fields)
}

func (r *DecisionsRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{
		TokenType:   types.TokenTypeTokenizer,
		CombineText: kitutil.JsonRawMessageToString(r.State) + "\n" + string(r.Questions),
	}
}

func (r *DecisionsRequest) IsStream(_ *http.Request) bool { return false }

func (r *DecisionsRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
