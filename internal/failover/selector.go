package failover

import "time"

type Selector struct {
	Config PolicyConfig
	Now    func() time.Time
}

func (s Selector) Select(nodes []Node, state State) Decision {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	return Evaluate(nodes, state, s.Config, now)
}
