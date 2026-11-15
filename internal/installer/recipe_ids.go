// Installer: the reference slice ids, named once.
package installer

// The ids appear in the unit table, the advise keywords and the docs. A typo in
// one of them is a slice that silently never matches, so they are constants
// rather than literals repeated across files.
const (
	recipeChat          = "chat"
	recipeKiosk         = "kiosk"
	recipeNotes         = "notes"
	recipeNotifications = "notifications"
	recipeReview        = "review"
	recipeRoom          = "room"
	recipeTodo          = "todo"
	recipeWhiteboard    = "whiteboard"
)
