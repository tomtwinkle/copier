package copier_test

import (
	"testing"

	"github.com/tomtwinkle/copier"
)

type EmployeeTags struct {
	Name         string `copier:"must"`
	DOB          string
	Address      string
	ID           int    `copier:"-"`
	FieldDifferA string `copier:"Differ1"`
	FieldDifferB string `copier:"Differ2"`
}

type User1 struct {
	Name         string
	DOB          string
	Address      string
	ID           int
	Differ1      string
	FieldDiffer2 string `copier:"Differ2"`
}

type User2 struct {
	DOB     string
	Address string
	ID      int
}

func TestCopyTagIgnore(t *testing.T) {
	employee := EmployeeTags{ID: 100}
	user := User1{Name: "Dexter Ledesma", DOB: "1 November, 1970", Address: "21 Jump Street", ID: 12345}
	copier.Copy(&employee, user)
	if employee.ID == user.ID {
		t.Error("Was not expected to copy IDs")
	}
	if employee.ID != 100 {
		t.Error("Original ID was overwritten")
	}
}

func TestCopyTagMust(t *testing.T) {
	employee := &EmployeeTags{}
	user := &User2{DOB: "1 January 1970"}
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected a panic.")
		}
	}()
	copier.Copy(employee, user)
}

func TestCopyTagFieldName(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		employee := EmployeeTags{ID: 100}
		user := User1{Name: "Dexter Ledesma", Differ1: "field differ 1", FieldDiffer2: "field differ 2"}
		copier.Copy(&employee, user)
		if employee.FieldDifferA != user.Differ1 {
			t.Error("Was not expected to copy FieldDiffer tags")
		}
		if employee.FieldDifferB != user.FieldDiffer2 {
			t.Error("Was not expected to copy FieldDiffer tags")
		}
	})

	t.Run("error first field tag name is lower", func(t *testing.T) {
		type ErrorStructTags struct {
			FieldDifferA string `copier:"differ1"`
		}
		employee := ErrorStructTags{}
		user := User1{Differ1: "field differ 1"}
		err := copier.Copy(&employee, user)
		if err == nil {
			t.Error("must error")
		}
		err = copier.Copy(&user, employee)
		if err == nil {
			t.Error("must error")
		}
	})
}
