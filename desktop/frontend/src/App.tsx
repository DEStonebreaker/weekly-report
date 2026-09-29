import {useEffect, useRef, useState} from 'react';
import {ConnectGoogle, DiscardReportPreparation, GetSetupData, InvalidateReportPreparation, ListTaskLists, PrepareReport, SaveSettings, SendPreparedReport} from '../wailsjs/go/main/App';
import {app, google} from '../wailsjs/go/models';
import {EventsOff, EventsOn} from '../wailsjs/runtime/runtime';

type Settings = {sender: string; subject: string; taskListId: string; startDate: string; endDate: string; windowDays: number; scheduleEnabled: boolean; scheduleTime: string; recipients: {name: string; address: string}[]};
type TaskList = {ID: string; Title: string};
type Preview = {sender: string; subject: string; taskListId: string; startDate: string; endDate: string; messages: {recipient: {name: string; address: string}; html: string}[]};
type Progress = {preparationId: string; completed: number; total: number; result?: {recipient: {name: string; address: string}; status: string; error?: string}};
type Delivery = {recipient: {name: string; address: string}; status: string; messageId?: string; error?: string};
type ScheduledRun = {time: string; status: string; results?: Delivery[]; error?: string};
type SchedulerStatus = {active: boolean; error?: string};

function emptySettings(): Settings {
  const today = new Date();
  const end = new Date(today);
  end.setDate(end.getDate() + 6);
  const date = (value: Date) => `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, '0')}-${String(value.getDate()).padStart(2, '0')}`;
  return {sender: '', subject: 'Weekly Report', taskListId: '', startDate: date(today), endDate: date(end), windowDays: 7, scheduleEnabled: false, scheduleTime: '09:00', recipients: []};
}

export default function App() {
  const [settings, setSettings] = useState<Settings>(emptySettings());
  const [settingsSaved, setSettingsSaved] = useState(false);
  const [connected, setConnected] = useState(false);
  const [credentialsPath, setCredentialsPath] = useState('');
  const [taskLists, setTaskLists] = useState<TaskList[]>([]);
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [rangePreset, setRangePreset] = useState('next7');
  const [customFrom, setCustomFrom] = useState('');
  const [customTo, setCustomTo] = useState('');
  const [preview, setPreview] = useState<Preview | null>(null);
  const [preparationId, setPreparationId] = useState('');
  const [selectedRecipient, setSelectedRecipient] = useState(0);
  const [progress, setProgress] = useState<Progress | null>(null);
  const [results, setResults] = useState<Delivery[]>([]);
  const [schedulerActive, setSchedulerActive] = useState(false);
  const [scheduledRun, setScheduledRun] = useState<ScheduledRun | null>(null);
  const revision = useRef(0);
  const preparationRef = useRef('');
  const activeSendRef = useRef('');

  async function refreshSetup() {
    try {
      const state = await GetSetupData();
      setSettings({...state.settings, recipients: state.settings.recipients ?? []});
      setSettingsSaved(state.settingsSaved);
      setConnected(state.googleConnected);
      setCredentialsPath(state.credentialsPath);
      setSchedulerActive(state.schedulerActive);
      setScheduledRun(state.lastScheduledRun ?? null);
      setError('');
    } catch (e) { setError(String(e)); }
  }

  async function loadLists() {
    setBusy('lists'); setError(''); setNotice('');
    try {
      const result = await ListTaskLists();
      setTaskLists(result ?? []);
      setNotice(result.length ? 'Task lists loaded. Choose the list for your reports.' : 'No task lists were found in this Google account.');
    } catch (e) { setError(String(e)); }
    finally { setBusy(''); }
  }

  async function connect() {
    invalidatePreview();
    setBusy('connect'); setError(''); setNotice('A Google sign-in page will open in your browser. Use the same Google account to renew your connection, then prepare a new preview.');
    try {
      await ConnectGoogle();
      setConnected(true);
      setNotice('Google is connected. Load your task lists to continue.');
      await loadLists();
    } catch (e) { setError(String(e)); }
    finally { setBusy(''); }
  }

  async function save() {
    setBusy('save'); setError(''); setNotice('');
    try {
      const outcome = await SaveSettings(new app.Config(settings));
      setSettingsSaved(outcome.saved);
      setSchedulerActive(outcome.schedulerActive);
      if (outcome.scheduleError) {
        setError(outcome.scheduleError);
        setNotice('Settings were saved, but automatic delivery could not be activated. Save again to retry.');
      } else if (settings.scheduleEnabled) {
        setNotice(outcome.schedulerActive ? `Settings saved. Automatic delivery is active every day at ${settings.scheduleTime}.` : 'Settings saved. Automatic delivery is not active.');
      } else {
        setNotice('Settings saved on this computer.');
      }
    } catch (e) { setError(String(e)); }
    finally { setBusy(''); }
  }

  function invalidatePreview() {
    revision.current += 1;
    const oldID = preparationRef.current;
    preparationRef.current = '';
    setPreparationId(''); setPreview(null); setResults([]); setProgress(null);
    if (oldID) void DiscardReportPreparation(oldID);
    void InvalidateReportPreparation(revision.current);
    setNotice(''); setError('');
  }

  async function prepare() {
    const requestedRevision = revision.current;
    setBusy('prepare'); setError(''); setNotice(''); setPreview(null); setResults([]); setProgress(null);
    try {
      const result = await PrepareReport(new app.Config(settings), rangePreset, customFrom, customTo, requestedRevision);
      if (revision.current !== requestedRevision) {
        void DiscardReportPreparation(result.id);
        return;
      }
      preparationRef.current = result.id;
      setPreparationId(result.id); setPreview(result.preview); setSelectedRecipient(0);
      setNotice('Review each personalized report below. Sending uses these exact prepared messages.');
    } catch (e) { setError(String(e)); }
    finally { setBusy(''); }
  }

  async function send() {
    const id = preparationRef.current;
    if (!id) return;
    preparationRef.current = '';
    activeSendRef.current = id;
    setPreparationId(''); setBusy('send'); setError(''); setNotice('Preparing to send the reviewed reports…'); setResults([]);
    try {
      const outcome = await SendPreparedReport(id);
      activeSendRef.current = '';
      setResults(outcome.results ?? []);
      if (outcome.error) {
        setError(outcome.error);
        setNotice((outcome.results ?? []).length && outcome.results.every((result: Delivery) => result.status === 'SKIPPED')
          ? 'No messages were sent. Reconnect Google if requested, then prepare a new preview.'
          : 'Review each recipient result. A new send requires a new preview.');
      }
      else setNotice('All recipient reports were sent.');
      if (outcome.results?.some((result: Delivery) => result.status === 'UNCERTAIN')) {
        setNotice('Some delivery outcomes are uncertain. Check Gmail Sent Mail before preparing another send.');
      }
    } catch (e) { setError(String(e)); }
    finally { activeSendRef.current = ''; setBusy(''); setProgress(null); }
  }

  useEffect(() => { void refreshSetup(); }, []);

  useEffect(() => {
    EventsOn('delivery-progress', (event: Progress) => {
      if (event.preparationId === preparationRef.current || event.preparationId === activeSendRef.current) {
        setProgress(event);
        if (event.result) setResults(current => {
          const index = current.findIndex(result => result.recipient.address === event.result?.recipient.address);
          if (index < 0) return [...current, event.result as Delivery];
          return current.map((result, i) => i === index ? event.result as Delivery : result);
        });
      }
    });
    EventsOn('scheduled-run-complete', (event: ScheduledRun) => {
      setScheduledRun(event);
      if (event.status === 'error' || event.status === 'schedule-error') setError(event.error ?? 'Scheduled delivery failed.');
      else if (event.status === 'complete') setNotice('Automatic delivery ran. See the latest scheduled result below.');
    });
    EventsOn('schedule-status', (event: SchedulerStatus) => {
      setSchedulerActive(event.active);
      if (event.error) setError(event.error);
    });
    return () => EventsOff('delivery-progress', 'scheduled-run-complete', 'schedule-status');
  }, []);

  function update<K extends keyof Settings>(key: K, value: Settings[K]) {
    if (busy === 'save' || busy === 'send') return;
    invalidatePreview();
    setSettings(current => ({...current, [key]: value}));
    setSettingsSaved(false);
  }

  function updateRecipient(index: number, key: 'name' | 'address', value: string) {
    const recipients = settings.recipients.map((recipient, i) => i === index ? {...recipient, [key]: value} : recipient);
    update('recipients', recipients);
  }

  function updateRangePreset(value: string) { if (busy === 'save' || busy === 'send') return; invalidatePreview(); setRangePreset(value); }
  function updateCustomDate(which: 'from' | 'to', value: string) {
    if (busy === 'save' || busy === 'send') return;
    invalidatePreview();
    if (which === 'from') setCustomFrom(value); else setCustomTo(value);
  }

  return (
    <main>
      <header><span className="mark" aria-hidden="true">W</span>Weekly Report<span className="badge">Setup</span></header>
      <section className="intro" aria-labelledby="welcome">
        <p className="eyebrow">One simple setup, then you're ready</p>
        <h1 id="welcome">Connect your tasks.<br/>Set up your report.</h1>
        <p className="lede">Sign in with Google, choose a task list, and tell Weekly Report who should receive each personalized email.</p>
      </section>

      {error && <div className="notice error" role="alert"><strong>Something needs attention</strong><span>{error}</span></div>}
      {notice && <div className="notice" role="status">{notice}</div>}

      <section className="panel setup-panel" aria-labelledby="google-heading">
        <div className="section-heading"><div><p className="step">STEP 1</p><h2 id="google-heading">Connect Google</h2></div><span className={`status ${connected ? '' : 'pending'}`} role="status">{connected ? 'Connected' : 'Not connected'}</span></div>
        <p>Weekly Report uses Google Tasks to prepare reports and Gmail to send them. Sign-in happens in your system browser.</p>
        {!connected && <div className="credential-help">Google OAuth credentials are not bundled yet. Add your Google Desktop app credentials file at:<code>{credentialsPath || 'Loading…'}</code></div>}
        <div className="actions"><button onClick={() => void connect()} disabled={!!busy}>{busy === 'connect' ? 'Waiting for Google…' : connected ? 'Reconnect Google' : 'Connect Google'}</button><button className="secondary" onClick={() => void loadLists()} disabled={!connected || !!busy}>{busy === 'lists' ? 'Loading lists…' : 'Load task lists'}</button></div>
      </section>

      <section className="panel settings-panel" aria-labelledby="settings-heading">
        <div className="section-heading"><div><p className="step">STEP 2</p><h2 id="settings-heading">Report settings</h2></div><span className={`status ${settingsSaved ? '' : 'pending'}`}>{settingsSaved ? 'Saved' : 'Unsaved'}</span></div>
        <div className="fields">
          <label className="field">Google task list
            <select value={settings.taskListId} onChange={e => update('taskListId', e.target.value)}>
              <option value="">Choose a task list</option>
              {taskLists.map(list => <option key={list.ID} value={list.ID}>{list.Title}</option>)}
              {settings.taskListId && !taskLists.some(list => list.ID === settings.taskListId) && <option value={settings.taskListId}>Saved list (load lists to verify)</option>}
            </select>
            <span className="hint">{connected ? 'Load task lists above if this menu is empty.' : 'Connect Google and load task lists first.'}</span>
          </label>
          <label className="field">Sender name and email<input value={settings.sender} onChange={e => update('sender', e.target.value)} placeholder="Your name <you@example.com>" autoComplete="email" /></label>
          <label className="field">Email subject<input value={settings.subject} onChange={e => update('subject', e.target.value)} /></label>
          <div className="field-grid">
            <label className="field">Schedule starts<input type="date" value={settings.startDate} onChange={e => update('startDate', e.target.value)} /></label>
            <label className="field">Schedule ends<input type="date" value={settings.endDate} onChange={e => update('endDate', e.target.value)} /></label>
          </div>
          <label className="field short-field">Report window length (days)<input type="number" min="1" value={settings.windowDays} onChange={e => update('windowDays', Number(e.target.value))} /><span className="hint">Scheduled reports repeat at this interval within the date range.</span></label>
          <div className="schedule-setting">
            <div className="schedule-choice"><div><strong>Automatic delivery</strong><span>Send the active scheduled report while Weekly Report is closed and you are logged into this Mac.</span></div><label className="toggle-label"><input type="checkbox" checked={settings.scheduleEnabled} onChange={e => update('scheduleEnabled', e.target.checked)} /><span>Enable</span></label></div>
            <label className="field schedule-time">Run each day at<input type="time" value={settings.scheduleTime} disabled={!settings.scheduleEnabled || busy === 'save'} onChange={e => update('scheduleTime', e.target.value)} /><span className="hint">Local time on this Mac</span></label>
            <p className="schedule-policy">If your Mac sleeps through the time, launchd runs the job when it wakes. If it was powered off, the next daily run is at the next scheduled time. Opening the app after the scheduled time also catches up, but only while a report window is active. A failed or uncertain recipient attempt is never retried automatically.</p>
            <div className="schedule-state"><span className={`status ${settings.scheduleEnabled && schedulerActive ? '' : 'pending'}`}>{settings.scheduleEnabled ? schedulerActive ? 'Background schedule active' : 'Background schedule not active' : 'Automatic delivery off'}</span>{settings.scheduleEnabled && !schedulerActive && <span className="hint">Save settings in the installed Weekly Report app to activate launchd.</span>}</div>
          </div>
        </div>

        <div className="recipients-heading"><div><h3>Recipients</h3><p>Each person receives a report with a personal greeting.</p></div><button className="secondary compact" onClick={() => update('recipients', [...settings.recipients, {name: '', address: ''}])}>Add recipient</button></div>
        {settings.recipients.length === 0 && <p className="empty">Add at least one recipient before saving.</p>}
        {settings.recipients.map((recipient, index) => <div className="recipient-row" key={index}>
          <label className="field">Name<input value={recipient.name} onChange={e => updateRecipient(index, 'name', e.target.value)} autoComplete="name" /></label>
          <label className="field">Email address<input type="email" value={recipient.address} onChange={e => updateRecipient(index, 'address', e.target.value)} autoComplete="email" /></label>
          <button className="remove" aria-label={`Remove recipient ${index + 1}`} onClick={() => update('recipients', settings.recipients.filter((_, i) => i !== index))}>Remove</button>
        </div>)}
        <div className="save-row"><span>{settingsSaved ? 'Your saved settings will be used next time you open the app.' : 'Changes are saved only on this computer.'}</span><button onClick={() => void save()} disabled={!!busy}>{busy === 'save' ? 'Saving…' : 'Save settings'}</button></div>
      </section>

      <section className="panel report-panel" aria-labelledby="report-heading">
        <div className="section-heading"><div><p className="step">STEP 3</p><h2 id="report-heading">Preview and send</h2></div><span className={`status ${preparationId ? '' : 'pending'}`}>{busy === 'send' ? 'Sending' : preparationId ? 'Ready to review' : results.length ? 'Send attempted' : 'No preview'}</span></div>
        <p>Choose dates, prepare personalized emails, and review each one before sending.</p>
        <div className="range-controls">
          <label className="field">Report dates<select value={rangePreset} onChange={e => updateRangePreset(e.target.value)}>
            <option value="next7">Next 7 days</option><option value="next14">Next 14 days</option><option value="thisWeek">This week (Monday–Sunday)</option><option value="nextWeek">Next week (Monday–Sunday)</option><option value="custom">Custom dates</option>
          </select></label>
          {rangePreset === 'custom' && <div className="field-grid"><label className="field">From<input type="date" value={customFrom} onChange={e => updateCustomDate('from', e.target.value)} /></label><label className="field">Through<input type="date" value={customTo} onChange={e => updateCustomDate('to', e.target.value)} /></label></div>}
        </div>
        <div className="preview-actions"><span className="hint">Save your settings and connect Google before preparing a report.</span><button onClick={() => void prepare()} disabled={!!busy || !settingsSaved || !connected || (rangePreset === 'custom' && (!customFrom || !customTo))}>{busy === 'prepare' ? 'Preparing report…' : 'Prepare preview'}</button></div>
        {busy === 'prepare' && <div className="progress-track" role="status"><span>Reading tasks and building each personalized message…</span><div className="progress-bar indeterminate"><i /></div></div>}
        {preview && <div className="preview-content">
          <div className="preview-summary"><div><strong>{preview.subject}</strong><span>{preview.startDate} through {preview.endDate} · {preview.messages.length} recipient{preview.messages.length === 1 ? '' : 's'}</span></div><label className="field recipient-select">Preview for<select value={selectedRecipient} onChange={e => setSelectedRecipient(Number(e.target.value))}>{preview.messages.map((message, index) => <option value={index} key={message.recipient.address}>{message.recipient.name} · {message.recipient.address}</option>)}</select></label></div>
          {preview.messages[selectedRecipient] && <iframe className="email-preview" title={`Personalized report preview for ${preview.messages[selectedRecipient].recipient.name}`} sandbox="" srcDoc={preview.messages[selectedRecipient].html} />}
          <div className="send-row"><span>This sends the exact message shown above to all {preview.messages.length} recipients.</span><button onClick={() => void send()} disabled={!preparationId || !!busy}>{busy === 'send' ? 'Sending…' : 'Send to recipients'}</button></div>
        </div>}
        {busy === 'send' && <div className="progress-track" role="status" aria-live="polite"><span>{progress && progress.total ? `Sending reports: ${progress.completed} of ${progress.total} complete…` : 'Connecting to Gmail…'}</span><div className="progress-bar"><i style={{width: `${progress?.total ? Math.round((progress.completed / progress.total) * 100) : 4}%`}} /></div></div>}
        {results.length > 0 && <div className="results" aria-labelledby="results-heading"><h3 id="results-heading">Delivery results</h3>{results.map((result, index) => <div className={`result-row ${result.status.toLowerCase()}`} key={`${result.recipient.address}-${index}`}><span className="result-icon" aria-hidden="true">{result.status === 'SENT' ? '✓' : result.status === 'UNCERTAIN' ? '?' : '!'}</span><div><strong>{result.recipient.name} · {result.recipient.address}</strong><span>{result.status === 'SENT' ? 'Sent successfully' : result.status === 'UNCERTAIN' ? 'Delivery is uncertain; check Gmail Sent Mail before retrying' : result.status === 'SKIPPED' ? 'Not sent' : 'Failed to send'}{result.error ? ` — ${result.error}` : ''}</span></div><span className="result-status">{result.status}</span></div>)}</div>}
      </section>

      {scheduledRun && <section className="panel scheduled-result" aria-labelledby="scheduled-result-heading"><div className="section-heading"><h2 id="scheduled-result-heading">Latest automatic run</h2><span className={`status ${scheduledRun.status === 'complete' ? '' : 'pending'}`}>{scheduledRun.status}</span></div><p>{new Date(scheduledRun.time).toLocaleString()}</p>{scheduledRun.error && <p className="schedule-error">{scheduledRun.error}</p>}{scheduledRun.results?.map((result, index) => <div className={`result-row ${result.status.toLowerCase()}`} key={`${result.recipient.address}-${index}`}><div className="result-icon" aria-hidden="true">{result.status === 'SENT' ? '✓' : result.status === 'UNCERTAIN' ? '?' : '!'}</div><div><strong>{result.recipient.name} · {result.recipient.address}</strong><span>{result.status}{result.error ? ` — ${result.error}` : ''}</span></div><span className="result-status">{result.status}</span></div>)}</section>}
      <p className="privacy">Your Google connection and report settings stay on this computer.</p>
    </main>
  );
}
