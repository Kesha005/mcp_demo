package store

import (
	"errors"
	"sync"
	"time"
)

type User struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

type Task struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Title     string `json:"title"`
	Status    string `json:"status"` // "todo" | "in_progress" | "done"
	CreatedAt string `json:"created_at"`
}

type Store struct {
	mu     sync.RWMutex
	users  map[string]User
	tasks  map[string]Task
	nextID int
}

func New() *Store {
	s := &Store{
		users: make(map[string]User),
		tasks: make(map[string]Task),
	}
	s.seed()
	return s
}

func (s *Store) seed() {
	u1 := s.CreateUser("Alice", "alice@example.com")
	u2 := s.CreateUser("Bob", "bob@example.com")
	s.CreateTask(u1.ID, "Write docs", "todo")
	s.CreateTask(u1.ID, "Fix bug #42", "in_progress")
	s.CreateTask(u2.ID, "Review PR", "todo")
}

func (s *Store) genID(prefix string) string {
	s.nextID++
	return prefix + "-" + time.Now().Format("20060102") + "-" + itoa(s.nextID)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

// ---------- Users ----------

func (s *Store) CreateUser(name, email string) User {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := User{
		ID:        s.genID("usr"),
		Name:      name,
		Email:     email,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.users[u.ID] = u
	return u
}

func (s *Store) ListUsers() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	return out
}

func (s *Store) GetUser(id string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return User{}, errors.New("user not found")
	}
	return u, nil
}

func (s *Store) DeleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return errors.New("user not found")
	}
	delete(s.users, id)
	// cascade delete tasks
	for tid, t := range s.tasks {
		if t.UserID == id {
			delete(s.tasks, tid)
		}
	}
	return nil
}

// ---------- Tasks ----------

func (s *Store) CreateTask(userID, title, status string) Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status == "" {
		status = "todo"
	}
	t := Task{
		ID:        s.genID("tsk"),
		UserID:    userID,
		Title:     title,
		Status:    status,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.tasks[t.ID] = t
	return t
}

func (s *Store) ListTasks(userID string) []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if userID == "" || t.UserID == userID {
			out = append(out, t)
		}
	}
	return out
}

func (s *Store) UpdateTaskStatus(id, status string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, errors.New("task not found")
	}
	t.Status = status
	s.tasks[id] = t
	return t, nil
}

func (s *Store) DeleteTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
		return errors.New("task not found")
	}
	delete(s.tasks, id)
	return nil
}