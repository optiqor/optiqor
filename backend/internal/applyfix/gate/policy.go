package gate

// SkeletonPolicy lets Passed and NotImplemented through; only Failed
// short-circuits. Phase-3 default.
type SkeletonPolicy struct{}

func (SkeletonPolicy) Allow(_ Stage, s Status) bool { return s != StatusFailed }

// StrictPolicy requires every stage Passes. Flip at Phase-5 boot.
type StrictPolicy struct{}

func (StrictPolicy) Allow(_ Stage, s Status) bool { return s == StatusPassed }
