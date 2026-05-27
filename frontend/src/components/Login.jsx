import { useState } from 'react'

// Login renders a username/password form and calls onLogin(token, streamUrl)
// on successful authentication. The token is never written to localStorage —
// it is passed directly to the parent, which owns the session state.
export default function Login({ onLogin }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  // error is shown below the form on failed auth or network failure.
  const [error, setError] = useState('')

  // handleSubmit posts credentials to the auth service and delegates
  // session ownership to the parent via onLogin. Keeping the token out of
  // this component matches the architecture: Login only proves identity,
  // App holds the session.
  async function handleSubmit(e) {
    e.preventDefault()
    setError('')

    try {
      const res = await fetch('http://localhost:8080/auth/token', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      })

      if (!res.ok) {
        // Auth service returned 401 — wrong credentials.
        setError('Invalid username or password')
        return
      }

      const data = await res.json()
      // Pass both credentials to App. stream_url is a base64 data URL containing
      // the rewritten M3U8 playlist — HLS.js loads it directly, do not modify it.
      onLogin(data.token, data.stream_url)
    } catch {
      // fetch threw — auth service is not reachable.
      setError('Could not reach server')
    }
  }

  return (
    <div style={{ maxWidth: 320, margin: '80px auto', fontFamily: 'sans-serif' }}>
      <h2 style={{ marginBottom: 24 }}>Lofi Stream</h2>
      <form onSubmit={handleSubmit}>
        {/* Controlled inputs — value is driven by state, not the DOM. */}
        <div style={{ marginBottom: 12 }}>
          <input
            type="text"
            placeholder="Username"
            value={username}
            onChange={e => setUsername(e.target.value)}
            style={{ width: '100%', padding: '8px', boxSizing: 'border-box' }}
          />
        </div>
        <div style={{ marginBottom: 12 }}>
          <input
            type="password"
            placeholder="Password"
            value={password}
            onChange={e => setPassword(e.target.value)}
            style={{ width: '100%', padding: '8px', boxSizing: 'border-box' }}
          />
        </div>
        <button type="submit" style={{ width: '100%', padding: '8px' }}>
          Login
        </button>
        {/* Render error only when one exists — empty string is falsy. */}
        {error && <p style={{ color: 'red', marginTop: 12 }}>{error}</p>}
      </form>
    </div>
  )
}
