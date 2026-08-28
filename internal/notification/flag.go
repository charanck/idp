package notification

// Enabled gates the entire notification feature (web UI, S2S API routes, and
// the background delivery worker) while it's still untested in production.
// Flip to true for gradual release; this is a code change, not a config one,
// so rollout is a deliberate deploy rather than an env var anyone can flip.
const Enabled = true
