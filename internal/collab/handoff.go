// What a collaborative recipe receives from the composition root.
package collab

// Handoff is the transport bundle a recipe takes from register.Deps.Collab.
//
// It exists so the seam package (internal/recipe/register, a CORE unit) can
// carry the transport without importing this SERVANT: the root passes a Handoff
// as `any`, and the recipe asserts back to it. The three values travel together
// because they always did — a persister with no store, or a store with no
// persister, is a half-wired collab that fails at the first write.
type Handoff struct {
	Persister Persister
	Docs      *DocStore
	Presence  PresenceSeam
}
