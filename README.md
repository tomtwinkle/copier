# Copier

  This is a function-added version of the original [copier](https://github.com/jinzhu/copier).

## Features

* Copy from field to field with same name
* Copy from method to field with same name
* Copy from field to method with same name
* Copy from slice to slice
* Copy from struct to slice
* Copy from map to map
* Enforce copying a field with a tag
* Ignore a field with a tag
* Deep Copy

## Usage

```go
package main

import (
	"fmt"
	"github.com/tomtwinkle/copier"
)

type User struct {
	Name      string
	Role      string
	Age       int32
	SkillSets string

	// Tell copier.Copy to coping this field as the set `reference to the dest field tag`.
	// must be start Upper case(public field).
	Message  string `copier:"CommentField"`

	// Explicitly ignored in the destination struct.
	Salary    int
}

func (user *User) DoubleAge() int32 {
	return 2 * user.Age
}

// Tags in the destination Struct provide instructions to copier.Copy to ignore
// or enforce copying and to panic or return an error if a field was not copied.
type Employee struct {
	// Tell copier.Copy to panic if this field is not copied.
	Name      string `copier:"must"`

	// Tell copier.Copy to return an error if this field is not copied.
	Age       int32  `copier:"must,nopanic"`

	// Tell copier.Copy to explicitly ignore copying this field.
	Salary    int    `copier:"-"`

	// Tell copier.Copy to coping this field as the set `SkillSets`.
	// must be start Upper case(public field).
    Skill     string `copier:"SkillSet"`

	// Tell copier.Copy to coping this field as the set `reference to the src field tag`.
	// must be start Upper case(public field).
	Comment   string `copier:"CommentField"`

	DoubleAge int32
	EmployeId int64
	SuperRole string
}

func (employee *Employee) Role(role string) {
	employee.SuperRole = "Super " + role
}

func main() {
	var (
		user      = User{Name: "tomtwinkle", Age: 18, Role: "Admin", Salary: 200000, SkillSets:"golang", Message: "test!"}
		users     = []User{{Name: "tomtwinkle", Age: 18, Role: "Admin", Salary: 100000}, {Name: "tomtwinkle 2", Age: 30, Role: "Dev", Salary: 60000}}
		employee  = Employee{Salary: 150000}
		employees = []Employee{}
	)

	copier.Copy(&employee, &user)

	fmt.Printf("%#v \n", employee)
	// Employee{
	//    Name: "tomtwinkle",       // Copy from field
	//    Age: 18,                  // Copy from field
	//    Salary:150000,            // Copying explicitly ignored
	//    Skill:"golang",           // Copying from field
	//    Comment:"test!",          // Copying from field
	//    DoubleAge: 36,            // Copy from method
	//    EmployeeId: 0,            // Ignored
	//    SuperRole: "Super Admin", // Copy to method
	// }

	// Copy struct to slice
	copier.Copy(&employees, &user)

	fmt.Printf("%#v \n", employees)
	// []Employee{
	//   {Name: "tomtwinkle", Age: 18, Salary:0, DoubleAge: 36, EmployeId: 0, SuperRole: "Super Admin"}
	// }

	// Copy slice to slice
	employees = []Employee{}
	copier.Copy(&employees, &users)

	fmt.Printf("%#v \n", employees)
	// []Employee{
	//   {Name: "tomtwinkle", Age: 18, Salary:0, DoubleAge: 36, EmployeId: 0, SuperRole: "Super Admin"},
	//   {Name: "tomtwinkle 2", Age: 30, Salary:0, DoubleAge: 60, EmployeId: 0, SuperRole: "Super Dev"},
	// }

 	// Copy map to map
	map1 := map[int]int{3: 6, 4: 8}
	map2 := map[int32]int8{}
	copier.Copy(&map2, map1)

	fmt.Printf("%#v \n", map2)
	// map[int32]int8{3:6, 4:8}
}
```

### Copy with Option

```go
copier.CopyWithOption(&to, &from, copier.Option{IgnoreEmpty: true, DeepCopy: true})
```

# Author

**tomtwinkle**

## License

Released under the [MIT License](https://github.com/tomtwinkle/copier/blob/master/License).
