import { useState } from 'react'
import Login from './components/Login'

// App is the top-level state machine with two states:
// unauthenticated — token is null, Login form is shown.
// authenticated   — token is set, Player is shown (placeholder until Task 8).
export default function App() {
  // token and streamUrl live here so both the Player (Task 8) and any future
  // components can access them. Neither is written to localStorage —
  // a page refresh intentionally requires re-authentication.
  const [token, setToken] = useState(null)
  const [streamUrl, setStreamUrl] = useState(null)

  // handleLogin transitions to authenticated state. Called by Login on success.
  // Both values are passed in a single callback to keep App as the single owner.
  function handleLogin(newToken, newStreamUrl) {
    setToken(newToken)
    setStreamUrl(newStreamUrl)
  }

  // Unauthenticated — show login form.
  if (!token) {
    return <Login onLogin={handleLogin} />
  }

  // Authenticated — Player component replaces this in Task 8.
  // streamUrl is passed to Player so HLS.js can load the R2 presigned URL.
  return <p style={{ fontFamily: 'sans-serif', padding: 40 }}>Logged in</p>
}
