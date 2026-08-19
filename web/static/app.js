(function () {
  'use strict';

  var state = {
    file: null,
    duration: 0,
    start: 0,
    end: 0,
    activeDragHandle: null,
    jobId: null,
    eventSource: null,
    lastPath: '',
  };

  var el = function (id) { return document.getElementById(id); };
  var clamp = function (v, lo, hi) { return Math.max(lo, Math.min(hi, v)); };

  function pad2(n) { return String(n).padStart(2, '0'); }
  function formatField(seconds) {
    if (!isFinite(seconds) || seconds < 0) seconds = 0;
    var m = Math.floor(seconds / 60);
    var s = seconds - m * 60;
    return pad2(m) + ':' + s.toFixed(1).padStart(4, '0');
  }
  function formatLabel(seconds) {
    seconds = Math.max(0, Math.floor(seconds || 0));
    var h = Math.floor(seconds / 3600);
    var m = Math.floor((seconds % 3600) / 60);
    var s = seconds % 60;
    if (h > 0) return h + ':' + pad2(m) + ':' + pad2(s);
    return pad2(m) + ':' + pad2(s);
  }
  function parseTimecode(input) {
    if (input == null) return NaN;
    var s = String(input).trim();
    if (s === '') return NaN;
    var parts = s.split(':');
    if (parts.length > 3) return NaN;
    for (var i = 0; i < parts.length; i++) {
      if (!/^\d+(\.\d+)?$/.test(parts[i])) return NaN;
    }
    var seconds = 0;
    for (var j = 0; j < parts.length; j++) seconds = seconds * 60 + parseFloat(parts[j]);
    return seconds;
  }

  // ---- drop zone ------------------------------------------------------------

  var dropZone = el('dropZone');
  var fileInput = el('fileInput');

  dropZone.addEventListener('click', function () { fileInput.click(); });
  fileInput.addEventListener('change', function () {
    if (fileInput.files && fileInput.files[0]) loadFile(fileInput.files[0]);
  });
  ['dragenter', 'dragover'].forEach(function (evt) {
    dropZone.addEventListener(evt, function (e) { e.preventDefault(); dropZone.classList.add('dragover'); });
  });
  ['dragleave', 'drop'].forEach(function (evt) {
    dropZone.addEventListener(evt, function (e) { e.preventDefault(); dropZone.classList.remove('dragover'); });
  });
  dropZone.addEventListener('drop', function (e) {
    if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0]) loadFile(e.dataTransfer.files[0]);
  });

  function loadFile(file) {
    hideError();
    state.file = file;
    var video = el('preview');
    video.src = URL.createObjectURL(file);
    video.addEventListener('loadedmetadata', function onMeta() {
      video.removeEventListener('loadedmetadata', onMeta);
      state.duration = video.duration || 0;
      state.start = 0;
      state.end = state.duration;
      dropZone.classList.add('hidden');
      el('workspace').classList.remove('hidden');
      el('labelZero').textContent = '00:00';
      el('labelDuration').textContent = formatLabel(state.duration);
      updateHandles();
      updateFields();
    });
    video.addEventListener('error', function () {
      showError("Couldn't read that file as a video.");
    });
  }

  el('startOver').addEventListener('click', function () {
    closeJob();
    state.file = null;
    el('preview').removeAttribute('src');
    el('workspace').classList.add('hidden');
    el('doneActions').classList.add('hidden');
    dropZone.classList.remove('hidden');
    fileInput.value = '';
  });

  // ---- timeline --------------------------------------------------------

  function setupTimeline() {
    var timeline = el('timeline');
    var handleStart = el('handleStart');
    var handleEnd = el('handleEnd');

    timeline.addEventListener('pointerdown', function (e) {
      if (e.target === handleStart || e.target === handleEnd) return;
      seekPreview(timeFromEvent(e));
    });

    [handleStart, handleEnd].forEach(function (handle) {
      var which = handle.dataset.handle;
      handle.addEventListener('pointerdown', function (e) {
        e.stopPropagation();
        handle.setPointerCapture(e.pointerId);
        state.activeDragHandle = which;
      });
      handle.addEventListener('pointermove', function (e) {
        if (state.activeDragHandle !== which) return;
        setHandleTime(which, timeFromEvent(e));
      });
      handle.addEventListener('pointerup', function () { state.activeDragHandle = null; });
      handle.addEventListener('keydown', function (e) {
        var step = e.shiftKey ? 5 : 1;
        var current = which === 'start' ? state.start : state.end;
        if (e.key === 'ArrowLeft') { setHandleTime(which, current - step); e.preventDefault(); }
        if (e.key === 'ArrowRight') { setHandleTime(which, current + step); e.preventDefault(); }
      });
    });
  }

  function timeFromEvent(e) {
    var rect = el('timeline').getBoundingClientRect();
    var ratio = rect.width ? clamp((e.clientX - rect.left) / rect.width, 0, 1) : 0;
    return ratio * state.duration;
  }

  function setHandleTime(which, t) {
    t = clamp(t, 0, state.duration);
    var minGap = Math.min(0.3, state.duration || 0.3);
    if (which === 'start') {
      state.start = Math.min(t, state.end - minGap);
      state.start = Math.max(0, state.start);
    } else {
      state.end = Math.max(t, state.start + minGap);
      state.end = Math.min(state.duration, state.end);
    }
    updateHandles();
    updateFields();
    seekPreview(which === 'start' ? state.start : state.end);
  }

  function updateHandles() {
    var d = state.duration || 1;
    var startPct = (state.start / d) * 100;
    var endPct = (state.end / d) * 100;
    el('handleStart').style.left = startPct + '%';
    el('handleEnd').style.left = endPct + '%';
    el('range').style.left = startPct + '%';
    el('range').style.width = Math.max(0, endPct - startPct) + '%';
  }

  function updateFields() {
    el('startField').value = formatField(state.start);
    el('endField').value = formatField(state.end);
    el('durationField').textContent = (state.end - state.start).toFixed(1);
  }

  el('startField').addEventListener('change', function () { onFieldChange('start'); });
  el('endField').addEventListener('change', function () { onFieldChange('end'); });

  function onFieldChange(which) {
    var field = el(which === 'start' ? 'startField' : 'endField');
    var t = parseTimecode(field.value);
    if (isNaN(t)) { updateFields(); return; }
    setHandleTime(which, t);
  }

  function seekPreview(t) {
    var video = el('preview');
    if (video.readyState > 0) video.currentTime = clamp(t, 0, state.duration || video.duration || 0);
    var d = state.duration || 1;
    el('playhead').style.left = ((t / d) * 100) + '%';
  }

  el('preview').addEventListener('timeupdate', function () {
    var video = el('preview');
    var d = state.duration || video.duration || 1;
    el('playhead').style.left = ((video.currentTime / d) * 100) + '%';
  });
  el('preview').addEventListener('click', function () {
    var video = el('preview');
    if (video.paused) video.play(); else video.pause();
  });

  setupTimeline();

  // ---- export ------------------------------------------------------------

  el('exportBtn').addEventListener('click', onExport);

  function onExport() {
    if (!state.file) return;
    hideError();
    el('doneActions').classList.add('hidden');
    setExporting(true);

    var form = new FormData();
    form.append('file', state.file, state.file.name);
    form.set('start', String(state.start));
    form.set('end', String(state.end));
    form.set('width', el('width').value);

    fetch('/api/jobs', { method: 'POST', body: form })
      .then(function (res) { return res.json().then(function (data) { if (!res.ok) throw new Error(data.error || 'request failed'); return data; }); })
      .then(function (data) {
        state.jobId = data.jobId;
        subscribeJob(data.jobId);
      })
      .catch(function (err) {
        setExporting(false);
        showError(String(err.message || err));
      });
  }

  function subscribeJob(jobId) {
    showProgress();
    var es = new EventSource('/api/jobs/' + jobId + '/events');
    state.eventSource = es;

    es.onmessage = function (msg) {
      var e = JSON.parse(msg.data);
      if (e.message) el('progressLabel').textContent = e.message;
      if (e.stage === 'done') {
        closeJob();
        setExporting(false);
        hideProgress();
        showDone(e);
      } else if (e.stage === 'error') {
        closeJob();
        setExporting(false);
        hideProgress();
        showError(e.code === 'missing-tool' ? e.message : (e.message || 'Something went wrong.'));
      } else if (e.stage === 'canceled') {
        closeJob();
        setExporting(false);
        hideProgress();
      }
    };
    es.onerror = function () {
      closeJob();
      setExporting(false);
      hideProgress();
      showError('Lost connection to the local server.');
    };
  }

  el('cancelJob').addEventListener('click', function () {
    if (!state.jobId) return;
    fetch('/api/jobs/' + state.jobId + '/cancel', { method: 'POST' }).catch(function () {});
  });

  function closeJob() {
    if (state.eventSource) { state.eventSource.close(); state.eventSource = null; }
    state.jobId = null;
  }

  function showDone(e) {
    el('doneActions').classList.remove('hidden');
    el('doneMessage').textContent = (e.filename || 'Saved.') + ' — saved to Downloads.';
    state.lastPath = e.path || '';
  }

  el('openFile').addEventListener('click', function () {
    if (state.lastPath) fetch('/api/open', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path: state.lastPath }) });
  });
  el('showFolder').addEventListener('click', function () {
    if (state.lastPath) fetch('/api/reveal', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path: state.lastPath }) });
  });

  // ---- small UI state helpers ----------------------------------------

  function setExporting(v) { el('exportBtn').disabled = v; }
  function showError(msg) { el('error').textContent = msg; el('error').classList.remove('hidden'); }
  function hideError() { el('error').classList.add('hidden'); }
  function showProgress() { el('progress').classList.remove('hidden'); el('progressLabel').textContent = 'Converting…'; }
  function hideProgress() { el('progress').classList.add('hidden'); }
})();
