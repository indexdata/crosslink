package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequestBinding(t *testing.T) {
	// Example nested struct
	type Address struct {
		Street string `form:"address.street"`
		City   string `form:"address.city"`
	}

	// Example struct with nested struct
	type User struct {
		Name    string `form:"user.name"`
		Pass    string `form:"user.pass"`
		Address Address
	}

	// Example form data
	form := map[string][]string{
		"user.name":      {"JohnDoe"},
		"user.pass":      {"secret"},
		"address.street": {"123 Main St"},
		"address.city":   {"Anytown"},
	}

	// Create an instance of the struct
	user := &User{}

	// Populate the struct with form data
	err := PopulateStructFromForm(user, form)
	if err != nil {
		assert.Nil(t, err)
	}

	// Output the populated struct
	assert.Equal(t, "JohnDoe", user.Name)
	assert.Equal(t, "secret", user.Pass)
	assert.Equal(t, "123 Main St", user.Address.Street)
	assert.Equal(t, "Anytown", user.Address.City)
}

// func TestRequestBindingNoTags(t *testing.T) {
// 	// Example nested struct
// 	type Address struct {
// 		Street string `form:"address.street"`
// 		City   string `form:"address.city"`
// 	}

// 	// Example struct with nested struct
// 	type User struct {
// 		Name    string `form:"user.name"`
// 		Pass    string `form:"user.pass"`
// 		Address Address
// 	}

// 	// Example form data
// 	form := map[string][]string{
// 		"User.Name":      {"JohnDoe"},
// 		"User.Pass":      {"secret"},
// 		"Address.Street": {"123 Main St"},
// 		"Address.City":   {"Anytown"},
// 	}

// 	// Create an instance of the struct
// 	user := &User{}

// 	// Populate the struct with form data
// 	errs := PopulateStructFromFormNoTags(user, form)
// 	assert.Empty(t, errs)

// 	// Output the populated struct
// 	assert.Equal(t, "JohnDoe", user.Name)
// 	assert.Equal(t, "secret", user.Pass)
// 	assert.Equal(t, "123 Main St", user.Address.Street)
// 	assert.Equal(t, "Anytown", user.Address.City)
// }
