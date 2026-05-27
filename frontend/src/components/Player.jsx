import { useEffect, useRef, useState } from 'react'
import Hls from 'hls.js'

// Player initialises HLS.js with the Cloudflare R2 presigned URL and exposes
// a play/pause toggle. The presigned URL carries SigV4 auth as query params —
// HLS.js uses it directly with no Authorization header required.
export default function Player({ streamUrl }) {
  // audioRef holds the DOM audio element. A ref is used instead of state
  // because the node must not trigger re-renders when it changes.
  const audioRef = useRef(null)

  // isPlaying drives the button label only — audio state lives in the DOM element.
  const [isPlaying, setIsPlaying] = useState(false)

  // Initialise HLS.js once on mount and tear it down on unmount.
  // streamUrl is in the dep array per React rules; in practice it never
  // changes after login, so this effect runs exactly once.
  useEffect(() => {
    const hls = new Hls()
    const audio = audioRef.current

    // Point HLS.js at the R2 presigned URL. loadSource fetches the M3U8;
    // attachMedia wires the decoded audio to the DOM element.
    // The URL must not be modified — its SigV4 signature covers the exact
    // query string returned by POST /auth/token.
    hls.loadSource(streamUrl)
    hls.attachMedia(audio)

    // Infinite loop: the static M3U8 has EXT-X-ENDLIST, so HLS.js stops
    // when the last segment plays out. Reloading the source on the ended
    // event restarts from seg000 without any server-side loop logic.
    function onEnded() {
      hls.loadSource(streamUrl)
      audio.play()
    }

    audio.addEventListener('ended', onEnded)

    // Clean up on unmount — remove the listener and destroy the HLS.js
    // instance to cancel any in-flight segment requests.
    return () => {
      audio.removeEventListener('ended', onEnded)
      hls.destroy()
    }
  }, [streamUrl])

  // togglePlayPause plays or pauses the audio element and syncs isPlaying.
  // play() returns a Promise that rejects if the browser blocks autoplay;
  // the catch guard resets isPlaying so the button label stays accurate.
  function togglePlayPause() {
    const audio = audioRef.current
    if (isPlaying) {
      audio.pause()
      setIsPlaying(false)
    } else {
      audio.play().catch(() => setIsPlaying(false))
      setIsPlaying(true)
    }
  }

  return (
    <div style={{ maxWidth: 320, margin: '80px auto', fontFamily: 'sans-serif' }}>
      <h2 style={{ marginBottom: 24 }}>Lofi Stream</h2>
      {/* Hidden audio element — playback is controlled via the ref, not rendered controls. */}
      <audio ref={audioRef} />
      <button onClick={togglePlayPause} style={{ padding: '8px 24px' }}>
        {isPlaying ? 'Pause' : 'Play'}
      </button>
    </div>
  )
}
