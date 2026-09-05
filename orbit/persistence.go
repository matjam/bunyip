package orbit

import (
	"encoding/json"

	"github.com/matjam/bunyip/ecs"
)

// Keep the original component field names for existing saves and prefabs.
// Elapsed is simulated seconds past Elements' epoch, after time scaling.
type keplerJSON struct {
	Primary  ecs.Entity `json:"Primary"`
	Elements Elements   `json:"Elements"`
	Mu       float64    `json:"Mu"`
	Elapsed  float64    `json:"Elapsed,omitempty"`
}

// MarshalJSON preserves the orbital phase in ECS saves and prefab JSON.
func (k Kepler) MarshalJSON() ([]byte, error) {
	return json.Marshal(keplerJSON{
		Primary: k.Primary, Elements: k.Elements, Mu: k.Mu, Elapsed: k.elapsed,
	})
}

// UnmarshalJSON restores the orbital phase. Older JSON without Elapsed
// starts at the epoch specified by Elements.
func (k *Kepler) UnmarshalJSON(data []byte) error {
	var saved keplerJSON
	if err := json.Unmarshal(data, &saved); err != nil {
		return err
	}
	*k = Kepler{
		Primary: saved.Primary, Elements: saved.Elements, Mu: saved.Mu, elapsed: saved.Elapsed,
	}
	return nil
}
