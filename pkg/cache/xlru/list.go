package xlru

// // element is an element of a linked list.
// type element struct {
// 	// Next and previous pointers in the doubly-linked list of elements.
// 	// To simplify the implementation, internally a list l is implemented
// 	// as a ring, such that &l.root is both the next element of the last
// 	// list element (l.Back()) and the previous element of the first list
// 	// element (l.Front()).
// 	next, prev *element
//
// 	// The value stored with this element.
// 	entry
// }

// list represents a doubly linked list.
// The zero value for list is an empty list ready to use.
type list[K comparable, V any] struct {
	root element[K, V] // sentinel list element, only &root, root.prev, and root.next are used
	len  int           // current list length excluding (this) sentinel element
}

// Init initializes or clears list l.
func (l *list[K, V]) Init() *list[K, V] {
	l.root.next = &l.root
	l.root.prev = &l.root
	l.len = 0
	return l
}

// New returns an initialized list.
func newList[K comparable, V any]() *list[K, V] {
	return new(list[K, V]).Init()
}

// Len returns the number of elements of list l.
// The complexity is O(1).
func (l *list[K, V]) Len() int { return l.len }

// Back returns the last element of list l or nil if the list is empty.
func (l *list[K, V]) Back() *element[K, V] {
	if l.len == 0 {
		return nil
	}
	return l.root.prev
}

// lazyInit lazily initializes a zero List value.
func (l *list[K, V]) lazyInit() {
	if l.root.next == nil {
		l.Init()
	}
}

// insert inserts e after at, increments l.len, and returns e.
func (l *list[K, V]) insert(e, at *element[K, V]) *element[K, V] {
	e.prev = at
	e.next = at.next
	e.prev.next = e
	e.next.prev = e
	l.len++
	return e
}

// insertValue is a convenience wrapper for insert(&Element{Value: v}, at).
func (l *list[K, V]) insertValue(key K, val V, at *element[K, V]) *element[K, V] {
	return l.insert(&element[K, V]{key: key, value: val}, at)
}

// remove removes e from its list, decrements l.len
func (l *list[K, V]) remove(e *element[K, V]) {
	e.prev.next = e.next
	e.next.prev = e.prev
	e.next = nil // avoid memory leaks
	e.prev = nil // avoid memory leaks
	l.len--
}

// move moves e to next to at.
func (l *list[K, V]) move(e, at *element[K, V]) {
	if e == at {
		return
	}
	e.prev.next = e.next
	e.next.prev = e.prev

	e.prev = at
	e.next = at.next
	e.prev.next = e
	e.next.prev = e
}

// Remove removes e from l if e is an element of list l.
// It returns the element value e.Value.
// The element must not be nil.
func (l *list[K, V]) Remove(e *element[K, V]) {
	l.remove(e)
}

// PushFront inserts a new element e with value v at the front of list l and returns e.
func (l *list[K, V]) PushFront(key K, val V) *element[K, V] {
	l.lazyInit()
	return l.insertValue(key, val, &l.root)
}

// MoveToFront moves element e to the front of list l.
// If e is not an element of l, the list is not modified.
// The element must not be nil.
func (l *list[K, V]) MoveToFront(e *element[K, V]) {
	if l.root.next == e {
		return
	}
	// see comment in List.Remove about initialization of l
	l.move(e, &l.root)
}
