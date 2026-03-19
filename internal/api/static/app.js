// Watchpost - Vanilla JS for WebRTC and UI helpers

let peerConnection = null;
let mjpegActive = false;

function getCameraName() {
  const params = new URLSearchParams(window.location.search);
  return params.get('name');
}

// WebRTC negotiation
async function startWebRTC() {
  const name = getCameraName();
  if (!name) return;

  stopStream();

  const config = {
    iceServers: [{ urls: 'stun:stun.l.google.com:19302' }]
  };

  peerConnection = new RTCPeerConnection(config);

  peerConnection.addTransceiver('video', { direction: 'recvonly' });

  peerConnection.ontrack = function(event) {
    const video = document.getElementById('live-video');
    video.srcObject = event.streams[0];
    video.style.display = 'block';

    const snap = document.getElementById('live-snapshot');
    if (snap) snap.style.display = 'none';

    const mjpeg = document.getElementById('live-mjpeg');
    if (mjpeg) mjpeg.style.display = 'none';
  };

  peerConnection.oniceconnectionstatechange = function() {
    if (peerConnection.iceConnectionState === 'failed' ||
        peerConnection.iceConnectionState === 'disconnected') {
      stopStream();
    }
  };

  try {
    const offer = await peerConnection.createOffer();
    await peerConnection.setLocalDescription(offer);

    const response = await fetch('/api/cameras/' + encodeURIComponent(name) + '/webrtc/offer', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(peerConnection.localDescription)
    });

    if (!response.ok) {
      throw new Error('WebRTC negotiation failed: ' + response.status);
    }

    const answer = await response.json();
    await peerConnection.setRemoteDescription(new RTCSessionDescription(answer));

    document.getElementById('btn-stop').style.display = '';
    document.getElementById('btn-webrtc').disabled = true;
    document.getElementById('btn-mjpeg').disabled = true;
  } catch (err) {
    console.error('WebRTC error:', err);
    stopStream();
  }
}

// MJPEG fallback
function startMJPEG() {
  const name = getCameraName();
  if (!name) return;

  stopStream();

  const mjpeg = document.getElementById('live-mjpeg');
  mjpeg.src = '/api/cameras/' + encodeURIComponent(name) + '/mjpeg';
  mjpeg.style.display = 'block';
  mjpegActive = true;

  const snap = document.getElementById('live-snapshot');
  if (snap) snap.style.display = 'none';

  const video = document.getElementById('live-video');
  if (video) video.style.display = 'none';

  document.getElementById('btn-stop').style.display = '';
  document.getElementById('btn-webrtc').disabled = true;
  document.getElementById('btn-mjpeg').disabled = true;
}

function stopStream() {
  // Stop WebRTC
  if (peerConnection) {
    peerConnection.close();
    peerConnection = null;
  }

  const video = document.getElementById('live-video');
  if (video) {
    video.srcObject = null;
    video.style.display = 'none';
  }

  // Stop MJPEG
  const mjpeg = document.getElementById('live-mjpeg');
  if (mjpeg) {
    mjpeg.src = '';
    mjpeg.style.display = 'none';
  }
  mjpegActive = false;

  // Show snapshot again
  const snap = document.getElementById('live-snapshot');
  if (snap) snap.style.display = 'block';

  const btnStop = document.getElementById('btn-stop');
  if (btnStop) btnStop.style.display = 'none';

  const btnWebrtc = document.getElementById('btn-webrtc');
  if (btnWebrtc) btnWebrtc.disabled = false;

  const btnMjpeg = document.getElementById('btn-mjpeg');
  if (btnMjpeg) btnMjpeg.disabled = false;
}

// Time formatting helpers
function formatTimeAgo(dateStr) {
  const d = new Date(dateStr);
  const now = new Date();
  const diff = (now - d) / 1000;

  if (diff < 60) return Math.floor(diff) + 's ago';
  if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
  if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
  return Math.floor(diff / 86400) + 'd ago';
}

function formatDateTime(dateStr) {
  const d = new Date(dateStr);
  return d.toLocaleString();
}
