import {useEffect, useRef, useState} from 'react';
import {ConnectGoogle, DiscardReportPreparation, GetSetupData, InvalidateReportPreparation, ListTaskLists, PrepareForceSend, PrepareReport, SaveSettings, SendPreparedReport} from '../wailsjs/go/main/App';
import {app, google} from '../wailsjs/go/models';
import {EventsOff, EventsOn} from '../wailsjs/runtime/runtime';

type Settings = {sender: string; subject: string; taskListId: string; startDate: string; endDate: string; windowDays: number; scheduleEnabled: boolean; scheduleTime: string; recipients: {name: string; address: string}[]};
type TaskList = {ID: string; Title: string};
type Preview = {sender: string; subject: string; taskListId: string; startDate: string; endDate: string; messages: {recipient: {name: string; address: string}; html: string}[]};
type Progress = {preparationId: string; completed: number; total: number; result?: {recipient: {name: string; address: string}; status: string; error?: string}};
type Delivery = {recipient: {name: string; address: string}; status: string; messageId?: string; error?: string};
type ScheduledRun = {time: string; status: string; source?: string; startDate?: string; endDate?: string; taskCount?: number; results?: Delivery[]; error?: string};
type ManualOutcome = {time: string; results: Delivery[]; complete: boolean};
type SchedulerStatus = {active: boolean; error?: string};
type SectionId = 'connect-google' | 'report-settings' | 'preview-send';
type View = 'setup' | 'activity';
type PreviewMode = 'manual' | 'force';

const sectionIds: SectionId[] = ['connect-google', 'report-settings', 'preview-send'];

function scheduledStatusLabel(status: string): string {
  switch (status) {
    case 'complete': return 'Delivery completed';
    case 'already-sent': return 'Already sent for this window';
    case 'no-tasks': return 'No tasks found; no email sent';
    case 'no-window': return 'Outside report dates';
    case 'schedule-error': return 'Schedule needs repair';
    default: return status;
  }
}

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
  const [toast, setToast] = useState<{message: string; kind: 'success' | 'info'} | null>(null);
  const [error, setError] = useState('');
  const [rangePreset, setRangePreset] = useState('next7');
  const [customFrom, setCustomFrom] = useState('');
  const [customTo, setCustomTo] = useState('');
  const [preview, setPreview] = useState<Preview | null>(null);
  const [preparationId, setPreparationId] = useState('');
  const [previewMode, setPreviewMode] = useState<PreviewMode>('manual');
  const [selectedRecipient, setSelectedRecipient] = useState(0);
  const [progress, setProgress] = useState<Progress | null>(null);
  const [results, setResults] = useState<Delivery[]>([]);
  const [latestManualOutcome, setLatestManualOutcome] = useState<ManualOutcome | null>(null);
  const [schedulerActive, setSchedulerActive] = useState(false);
  const [scheduledRun, setScheduledRun] = useState<ScheduledRun | null>(null);
  const [activeSection, setActiveSection] = useState<SectionId>('connect-google');
  const [view, setView] = useState<View>('setup');
  const [sidebarExpanded, setSidebarExpanded] = useState(true);
  const revision = useRef(0);
  const preparationRef = useRef('');
  const activeSendRef = useRef('');

  function showToast(message: string, kind: 'success' | 'info' = 'success') {
    setToast({message, kind});
  }

  function navigate(section: SectionId | 'activity') {
    if (section === 'activity') {
      setView('activity');
      window.scrollTo({top: 0, behavior: 'smooth'});
      return;
    }
    setView('setup');
    setActiveSection(section);
    requestAnimationFrame(() => document.getElementById(section)?.scrollIntoView({behavior: 'smooth', block: 'start'}));
  }

  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), 4800);
    return () => window.clearTimeout(timer);
  }, [toast]);

  async function refreshSetup() {
    if (!('go' in window)) return;
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
    setTaskLists([]);
    try {
      const result = await ListTaskLists();
      const lists = result ?? [];
      setTaskLists(lists);
      if (lists.length) showToast('Task lists loaded. Choose the list for your reports.');
      else setNotice('No task lists were found in this Google account.');
    } catch (e) { setError(String(e)); }
    finally { setBusy(''); }
  }

  async function connect() {
    invalidatePreview();
    setTaskLists([]);
    setBusy('connect'); setError(''); setNotice('A Google sign-in page will open in your browser. Use the same Google account to renew your connection, then prepare a new preview.');
    try {
      await ConnectGoogle();
      setConnected(true);
      setNotice('');
      showToast('Google connected. Load your task lists to continue.');
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
        if (outcome.schedulerActive) showToast(`Settings saved. Automatic delivery is active every day at ${settings.scheduleTime}.`);
        else setNotice('Settings saved. Automatic delivery is not active.');
      } else {
        showToast('Settings saved on this computer.');
      }
    } catch (e) { setError(String(e)); }
    finally { setBusy(''); }
  }

  function clearPreview(mode: PreviewMode = 'manual') {
    const oldID = preparationRef.current;
    preparationRef.current = '';
    setPreparationId(''); setPreview(null); setResults([]); setProgress(null);
    setPreviewMode(mode);
    if (oldID) void DiscardReportPreparation(oldID);
  }

  function invalidatePreview() {
    revision.current += 1;
    clearPreview();
    void InvalidateReportPreparation(revision.current);
    setNotice(''); setError('');
  }

  async function prepare() {
    const requestedRevision = revision.current;
    clearPreview();
    setBusy('prepare'); setError(''); setNotice('');
    try {
      const result = await PrepareReport(new app.Config(settings), rangePreset, customFrom, customTo, requestedRevision);
      if (revision.current !== requestedRevision) {
        void DiscardReportPreparation(result.id);
        return;
      }
      preparationRef.current = result.id;
      setPreparationId(result.id); setPreview(result.preview); setSelectedRecipient(0);
      showToast('Preview ready. Review each personalized report before sending.');
    } catch (e) { setError(String(e)); }
    finally { setBusy(''); }
  }

  async function prepareForceSend() {
    const requestedRevision = revision.current;
    clearPreview('force');
    setBusy('prepare-force'); setError(''); setNotice('');
    try {
      const result = await PrepareForceSend(rangePreset, customFrom, customTo, requestedRevision);
      if (revision.current !== requestedRevision) {
        void DiscardReportPreparation(result.id);
        return;
      }
      preparationRef.current = result.id;
      setPreparationId(result.id); setPreview(result.preview); setSelectedRecipient(0);
      showToast('Force send preview ready. Review each message before sending.');
    } catch (e) { setError(String(e)); }
    finally { setBusy(''); }
  }

  async function send() {
    const id = preparationRef.current;
    if (!id) return;
    const forceSend = previewMode === 'force';
    preparationRef.current = '';
    activeSendRef.current = id;
    setPreparationId(''); setBusy('send'); setError(''); setNotice('Preparing to send the reviewed reports…'); setResults([]);
    try {
      const outcome = await SendPreparedReport(id);
      activeSendRef.current = '';
      const deliveryResults = outcome.results ?? [];
      const complete = !outcome.error && deliveryResults.length > 0 && deliveryResults.length === preview?.messages.length && deliveryResults.every((result: Delivery) => result.status === 'SENT');
      setResults(deliveryResults);
      setLatestManualOutcome({time: new Date().toISOString(), results: deliveryResults, complete});
      if (outcome.error) {
        setError(outcome.error);
        setNotice(deliveryResults.length && deliveryResults.every((result: Delivery) => result.status === 'SKIPPED')
          ? 'No messages were sent. Reconnect Google if requested, then prepare a new preview.'
          : 'Review each recipient result. A new send requires a new preview.');
      }
      else if (complete) {
        setNotice('');
        showToast(forceSend ? 'Reports sent. Automatic delivery records were not changed.' : 'All recipient reports were sent.');
      }
      else setNotice('Review each recipient result. A new send requires a new preview.');
      if (deliveryResults.some((result: Delivery) => result.status === 'UNCERTAIN')) {
        setNotice('Some delivery outcomes are uncertain. Check Gmail Sent Mail before preparing another send.');
      }
      setView('activity');
      window.scrollTo({top: 0, behavior: 'smooth'});
    } catch (e) { setError(String(e)); }
    finally { activeSendRef.current = ''; setBusy(''); setProgress(null); }
  }

  useEffect(() => { void refreshSetup(); }, []);

  useEffect(() => {
    if (view !== 'setup') return;
    let frame = 0;
    const updateActiveSection = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        if (window.scrollY + window.innerHeight >= document.documentElement.scrollHeight - 4) {
          setActiveSection(sectionIds[sectionIds.length - 1]);
          return;
        }
        const current = sectionIds.reduce<SectionId>((active, id) => {
          const section = document.getElementById(id);
          return section && section.getBoundingClientRect().top <= 180 ? id : active;
        }, sectionIds[0]);
        setActiveSection(current);
      });
    };
    updateActiveSection();
    window.addEventListener('scroll', updateActiveSection, {passive: true});
    window.addEventListener('resize', updateActiveSection);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener('scroll', updateActiveSection);
      window.removeEventListener('resize', updateActiveSection);
    };
  }, [view]);

  useEffect(() => {
    if (!('runtime' in window)) return;
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
      else if (event.status === 'complete') showToast('Automatic delivery ran. View the latest result in Delivery activity.');
      else if (event.status === 'already-sent') setNotice('This report window was already sent; no new email was sent. See Delivery activity for details.');
      else if (event.status === 'no-tasks') setNotice('No dated tasks were found for this report window; no email was sent. See Delivery activity for details.');
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

  const sendComplete = busy !== 'send' && results.length > 0 && results.length === preview?.messages.length && results.every(result => result.status === 'SENT');
  const steps: {id: SectionId | 'activity'; title: string; complete: boolean}[] = [
    {id: 'connect-google', title: 'Connect Google', complete: connected},
    {id: 'report-settings', title: 'Report settings', complete: settingsSaved},
    {id: 'preview-send', title: 'Preview and send', complete: sendComplete},
    {id: 'activity', title: 'Delivery activity', complete: !!scheduledRun && (scheduledRun.status === 'complete' || scheduledRun.status === 'already-sent') || !!latestManualOutcome?.complete},
  ];

  return (
    <main className={`app-shell ${sidebarExpanded ? '' : 'sidebar-collapsed'}`}>
      <aside className="sidebar" aria-label="Report workflow">
        <button
          type="button"
          className="sidebar-toggle"
          aria-label={sidebarExpanded ? 'Collapse sidebar' : 'Expand sidebar'}
          aria-expanded={sidebarExpanded}
          aria-controls="sidebar-navigation"
          onClick={() => setSidebarExpanded(expanded => !expanded)}
        >
          <span aria-hidden="true">{sidebarExpanded ? '‹' : '›'}</span>
        </button>
        <div id="sidebar-navigation" className="sidebar-inner" aria-hidden={!sidebarExpanded} inert={!sidebarExpanded}>
          <div className="brand"><strong>Weekly Report</strong></div>
          <nav className="workflow-nav" aria-label="Report navigation">
            <ol>
              {steps.map(step => <li key={step.id}>
                <button type="button" className={`nav-link ${view === 'activity' ? step.id === 'activity' ? 'active' : '' : activeSection === step.id ? 'active' : ''}`} aria-current={view === 'activity' ? step.id === 'activity' ? 'page' : undefined : activeSection === step.id ? 'location' : undefined} onClick={() => navigate(step.id)}>
                  <span className="nav-copy"><strong>{step.title}</strong></span>
                  {step.complete && <span className="nav-check" role="img" aria-label={`${step.title} complete`}>✓</span>}
                </button>
              </li>)}
            </ol>
          </nav>
          <div className="sidebar-bottom">
            <a className="sidebar-tutorial" href="#tutorial" onClick={event => { event.preventDefault(); showToast('Tutorial coming soon.', 'info'); }}>Tutorial</a>
            <p className="sidebar-footnote">Your Google connection and report settings stay on this computer.</p>
          </div>
        </div>
      </aside>
      <div className="content">
      {error && <div className="notice error global-message" role="alert"><strong>Something needs attention</strong><span>{error}</span></div>}
      {notice && <div className="notice global-message" role="status">{notice}</div>}
      {view === 'setup' ? <>
      <section className="intro" aria-labelledby="welcome">
        <h1 id="welcome">Set up your report</h1>
        <p className="lede">Sign in with Google, choose a task list, and tell Weekly Report who should receive each personalized email.</p>
      </section>

      <section id="connect-google" className="panel setup-panel" aria-labelledby="google-heading">
        <div className="section-heading"><div><p className="step" role="status">Google Account: {connected ? <span className="inline-check"><span aria-hidden="true">✓</span> Connected</span> : 'Not connected'}</p><h2 id="google-heading">Connect Google</h2></div></div>
        <p>Weekly Report uses Google Tasks to prepare reports and Gmail to send them. Sign-in happens in your system browser.</p>
        {!connected && <div className="credential-help">Google OAuth credentials are not bundled yet. Add your Google Desktop app credentials file at:<code>{credentialsPath || 'Loading…'}</code></div>}
        <div className="actions"><button onClick={() => void connect()} disabled={!!busy}>{busy === 'connect' ? 'Waiting for Google…' : connected ? 'Reconnect Google' : 'Connect Google'}</button><div className="action-with-check"><button className="secondary" onClick={() => void loadLists()} disabled={!connected || !!busy}>{busy === 'lists' ? 'Loading lists…' : 'Load task lists'}</button>{taskLists.length > 0 && <span className="inline-check" role="status"><span aria-hidden="true">✓</span> Lists ready</span>}</div></div>
      </section>

      <section id="report-settings" className="panel settings-panel" aria-labelledby="settings-heading">
        <div className="section-heading"><div><p className="step">Your report</p><h2 id="settings-heading">Report settings</h2></div></div>
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
            <details className="schedule-details"><summary>How automatic delivery works</summary><p>If your Mac sleeps through the time, launchd runs the job when it wakes. If it was powered off, the next daily run is at the next scheduled time. Opening the app after the scheduled time also catches up, but only while a report window is active. A failed or uncertain recipient attempt is never retried automatically.</p></details>
            <div className="schedule-state">{settings.scheduleEnabled && schedulerActive ? <span className="inline-check" role="status"><span aria-hidden="true">✓</span> Background schedule active</span> : <span className="hint">{settings.scheduleEnabled ? 'Save settings in the installed Weekly Report app to activate the background schedule.' : 'Automatic delivery is off.'}</span>}</div>
          </div>
        </div>

        <div className="recipients-heading"><div><h3>Recipients</h3><p>Each person receives a report with a personal greeting.</p></div><button className="secondary compact" onClick={() => update('recipients', [...settings.recipients, {name: '', address: ''}])}>Add recipient</button></div>
        {settings.recipients.length === 0 && <p className="empty">Add at least one recipient before saving.</p>}
        {settings.recipients.map((recipient, index) => <div className="recipient-row" key={index}>
          <label className="field">Name<input value={recipient.name} onChange={e => updateRecipient(index, 'name', e.target.value)} autoComplete="name" /></label>
          <label className="field">Email address<input type="email" value={recipient.address} onChange={e => updateRecipient(index, 'address', e.target.value)} autoComplete="email" /></label>
          <button className="remove" aria-label={`Remove recipient ${index + 1}`} onClick={() => update('recipients', settings.recipients.filter((_, i) => i !== index))}>Remove</button>
        </div>)}
        <div className="save-row"><span>{settingsSaved ? 'Your saved settings will be used next time you open the app.' : 'Changes are saved only on this computer.'}</span><div className="action-with-check"><button onClick={() => void save()} disabled={!!busy}>{busy === 'save' ? 'Saving…' : 'Save settings'}</button>{settingsSaved && <span className="inline-check" role="status"><span aria-hidden="true">✓</span> Saved</span>}</div></div>
      </section>

      <section id="preview-send" className="panel report-panel" aria-labelledby="report-heading">
        <div className="section-heading"><div><p className="step">Delivery</p><h2 id="report-heading">Preview and send</h2></div></div>
        <p>Choose dates, prepare personalized emails, and review each one before sending.</p>
        <div className="range-controls">
          <label className="field">Report dates<select value={rangePreset} onChange={e => updateRangePreset(e.target.value)}>
            <option value="next7">Next 7 days</option><option value="next14">Next 14 days</option><option value="thisWeek">This week (Monday–Sunday)</option><option value="nextWeek">Next week (Monday–Sunday)</option><option value="custom">Custom dates</option>
          </select></label>
          {rangePreset === 'custom' && <div className="field-grid"><label className="field">From<input type="date" value={customFrom} onChange={e => updateCustomDate('from', e.target.value)} /></label><label className="field">Through<input type="date" value={customTo} onChange={e => updateCustomDate('to', e.target.value)} /></label></div>}
        </div>
        <div className="preview-actions"><span className="hint">Save your settings and connect Google before preparing a report.</span><button onClick={() => void prepare()} disabled={!!busy || !settingsSaved || !connected || (rangePreset === 'custom' && (!customFrom || !customTo))}>{busy === 'prepare' ? 'Preparing report…' : 'Prepare preview'}</button></div>
        <div className="force-send-option"><div><strong>Force send the selected report dates</strong><span>Uses saved settings and the dates above. It may duplicate an automatic email, but does not change scheduled delivery records.</span></div><button className="secondary" onClick={() => void prepareForceSend()} disabled={!!busy || !settingsSaved || !connected || (rangePreset === 'custom' && (!customFrom || !customTo))}>{busy === 'prepare-force' ? 'Preparing report…' : 'Review force send'}</button></div>
        {(busy === 'prepare' || busy === 'prepare-force') && <div className="progress-track" role="status"><span>Reading tasks and building each personalized message…</span><div className="progress-bar indeterminate"><i /></div></div>}
        {preview && <div className="preview-content">
          <div className="preview-summary"><div>{previewMode === 'force' && <span className="preview-mode">Force send · selected report dates</span>}<strong>{preview.subject}</strong><span>{preview.startDate} through {preview.endDate} · {preview.messages.length} recipient{preview.messages.length === 1 ? '' : 's'}</span></div><label className="field recipient-select">Preview for<select value={selectedRecipient} onChange={e => setSelectedRecipient(Number(e.target.value))}>{preview.messages.map((message, index) => <option value={index} key={message.recipient.address}>{message.recipient.name} · {message.recipient.address}</option>)}</select></label></div>
          {preview.messages[selectedRecipient] && <iframe className="email-preview" title={`Personalized report preview for ${preview.messages[selectedRecipient].recipient.name}`} sandbox="" srcDoc={preview.messages[selectedRecipient].html} />}
          <div className="send-row"><span>{previewMode === 'force' ? `This sends the reviewed report to all ${preview.messages.length} recipients now. It does not mark the automatic run as complete.` : `This sends the exact message shown above to all ${preview.messages.length} recipients.`}</span><button onClick={() => void send()} disabled={!preparationId || !!busy}>{busy === 'send' ? 'Sending…' : previewMode === 'force' ? 'Force send this report' : 'Send to recipients'}</button></div>
        </div>}
        {busy === 'send' && <div className="progress-track" role="status" aria-live="polite"><span>{progress && progress.total ? `Sending reports: ${progress.completed} of ${progress.total} complete…` : 'Connecting to Gmail…'}</span><div className="progress-bar"><i style={{width: `${progress?.total ? Math.round((progress.completed / progress.total) * 100) : 4}%`}} /></div></div>}
      </section>
      </> : <>
      <section className="intro activity-intro" aria-labelledby="activity-heading">
        <p className="step">Report history</p>
        <h1 id="activity-heading">Delivery activity</h1>
        <p className="lede">Review the latest manual send and automatic run in one place.</p>
      </section>
      <section className="panel activity-panel" aria-labelledby="manual-results-heading">
        <div className="section-heading"><div><p className="step">Manual send</p><h2 id="manual-results-heading">Latest send</h2></div>{latestManualOutcome?.complete && <span className="inline-check"><span aria-hidden="true">✓</span> Delivered</span>}</div>
        {latestManualOutcome ? <><p className="session-note">{new Date(latestManualOutcome.time).toLocaleString()} · Available during this app session</p>{latestManualOutcome.results.length > 0 ? <div className="results">{latestManualOutcome.results.map((result, index) => <div className={`result-row ${result.status.toLowerCase()}`} key={`${result.recipient.address}-${index}`}><span className="result-icon" aria-hidden="true">{result.status === 'SENT' ? '✓' : result.status === 'UNCERTAIN' ? '?' : '!'}</span><div><strong>{result.recipient.name} · {result.recipient.address}</strong><span>{result.status === 'SENT' ? 'Sent successfully' : result.status === 'UNCERTAIN' ? 'Delivery is uncertain; check Gmail Sent Mail before retrying' : result.status === 'SKIPPED' ? 'Not sent' : 'Failed to send'}{result.error ? ` — ${result.error}` : ''}</span></div><span className="result-status">{result.status}</span></div>)}</div> : <p className="activity-empty">No recipient results were returned for this send. Review the message above before trying again.</p>}</> : <p className="activity-empty">No manual delivery results yet. Prepare a preview to send your first report.</p>}
      </section>
      <section className="panel activity-panel scheduled-result" aria-labelledby="scheduled-result-heading">
        <div className="section-heading"><div><p className="step">Automatic delivery</p><h2 id="scheduled-result-heading">Latest automatic run</h2></div></div>
        {scheduledRun ? <><p className="run-meta"><strong>{scheduledStatusLabel(scheduledRun.status)}</strong><span>{new Date(scheduledRun.time).toLocaleString()}{scheduledRun.source ? ` · ${scheduledRun.source === 'launchd' ? 'Background schedule' : scheduledRun.source === 'gui-catch-up' ? 'App catch-up' : scheduledRun.source}` : ''}</span>{scheduledRun.startDate && scheduledRun.endDate && <span>{scheduledRun.startDate} through {scheduledRun.endDate}</span>}{scheduledRun.taskCount !== undefined && <span>{scheduledRun.taskCount} dated task{scheduledRun.taskCount === 1 ? '' : 's'}</span>}</p>{scheduledRun.error && <p className="schedule-error">{scheduledRun.error}</p>}{scheduledRun.results?.map((result, index) => <div className={`result-row ${result.status.toLowerCase()}`} key={`${result.recipient.address}-${index}`}><span className="result-icon" aria-hidden="true">{result.status === 'SENT' || result.status === 'SKIPPED' && !result.error ? '✓' : result.status === 'UNCERTAIN' ? '?' : '!'}</span><div><strong>{result.recipient.name} · {result.recipient.address}</strong><span>{result.status === 'SKIPPED' && !result.error ? 'Already sent in this report window' : result.status}{result.error ? ` — ${result.error}` : ''}</span></div><span className="result-status">{result.status}</span></div>)}</> : <p className="activity-empty">No automatic runs yet. Once automatic delivery runs, its latest result will appear here.</p>}
      </section>
      </>}
      </div>
      {toast && <div className="toast" role="status" aria-live="polite"><span className="toast-icon" aria-hidden="true">{toast.kind === 'success' ? '✓' : 'i'}</span><span>{toast.message}</span></div>}
    </main>
  );
}
