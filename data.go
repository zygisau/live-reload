package main

type Todo struct {
	Title string
	Done  bool
}

type TodoPageData struct {
	Host      string
	PageTitle string
	Todos     []Todo
}

func getData(host string) TodoPageData {
	return TodoPageData{
		Host:      host,
		PageTitle: "My TODO list",
		Todos: []Todo{
			{Title: "Task 1", Done: false},
			{Title: "Task 2", Done: true},
			{Title: "Task 3", Done: true},
		},
	}
}
