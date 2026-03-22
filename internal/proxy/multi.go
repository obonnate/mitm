package proxy

// MultiHandler returns a Handler that calls each of the provided handlers
// in order. This lets you wire the store, the bus, and any custom handlers
// together without coupling them.
func MultiHandler(handlers ...Handler) Handler {
	return func(ex *Exchange) {
		for _, h := range handlers {
			if h != nil {
				h(ex)
			}
		}
	}
}
