<script>
  import { onMount } from 'svelte';
  import FileTree from './FileTree.svelte';

  let project = null;
  let activeFile = '';
  let source = '';
  let savedSource = '';
  let payload = '{}';
  let output = '';
  let explanation = null;
  let view = 'run';
  let errorMessage = '';
  let statusMessage = '';
  let busy = '';
  $: isDirty = source !== savedSource;

  onMount(async () => {
    try {
      project = await request('/api/project');
      activeFile = project.file;
      source = project.source;
      savedSource = project.source;
      statusMessage = 'Ready';
    } catch (error) {
      errorMessage = error.message;
    }
  });

  async function request(path, options = {}) {
    let response;
    try {
      response = await fetch(path, {
        ...options,
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', ...(options.headers || {}) }
      });
    } catch (error) {
      throw new Error(`Cannot reach Flow Studio: ${error.message}`);
    }
    let data;
    try {
      data = await response.json();
    } catch {
      throw new Error(`Flow Studio returned an unreadable response (HTTP ${response.status}).`);
    }
    if (!response.ok) {
      const error = new Error(data.error || `Request failed (HTTP ${response.status}).`);
      error.output = data.output || '';
      throw error;
    }
    return data;
  }

  async function save() {
    await request('/api/save', { method: 'POST', body: JSON.stringify({ path: activeFile, source }) });
    savedSource = source;
    project = await request('/api/project');
    statusMessage = 'Saved';
  }

  async function openFile(path) {
    if (!project || path === activeFile || busy) return;
    errorMessage = '';
    statusMessage = '';
    busy = 'open';
    try {
      if (isDirty) await save();
      const result = await request(`/api/file?path=${encodeURIComponent(path)}`);
      activeFile = result.file;
      source = result.source;
      savedSource = result.source;
      view = 'run';
      statusMessage = `Opened ${activeFile}`;
    } catch (error) {
      errorMessage = error.message || 'Could not open the Flow file.';
    } finally {
      busy = '';
    }
  }

  async function perform(action) {
    errorMessage = '';
    statusMessage = '';
    output = '';
    view = 'run';
    busy = action;
    try {
      if (isDirty) await save();
      let body = {};
      if (action === 'run') {
        let parsedPayload;
        try {
          parsedPayload = JSON.parse(payload);
        } catch (error) {
          throw new Error(`Payload is not valid JSON: ${error.message}`);
        }
        if (parsedPayload === null || Array.isArray(parsedPayload) || typeof parsedPayload !== 'object') {
          throw new Error('Run payload must be a JSON object.');
        }
        body = { payload: parsedPayload };
      }
      const result = await request(`/api/${action}`, { method: 'POST', body: JSON.stringify(body) });
      output = pretty(result.output || 'Completed with no output.');
      statusMessage = action === 'validate' ? 'Validation passed' : 'Run completed';
    } catch (error) {
      errorMessage = error.message || 'The Studio action failed.';
      output = error.output ? pretty(error.output) : '';
    } finally {
      busy = '';
    }
  }

  async function showExplain() {
    errorMessage = '';
    statusMessage = '';
    busy = 'explain';
    try {
      if (isDirty) await save();
      explanation = await request('/api/explain');
      view = 'explain';
      statusMessage = 'Flow plans loaded';
    } catch (error) {
      errorMessage = error.message || 'Could not explain the Flow project.';
      output = error.output ? pretty(error.output) : '';
    } finally {
      busy = '';
    }
  }

  async function saveOnly() {
    errorMessage = '';
    statusMessage = '';
    busy = 'save';
    try {
      await save();
    } catch (error) {
      errorMessage = error.message || 'Could not save the Flow file.';
    } finally {
      busy = '';
    }
  }

  function pretty(value) {
    if (typeof value !== 'string') return JSON.stringify(value, null, 2);
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
</script>

<svelte:head>
  <title>Flow Studio</title>
  <meta name="viewport" content="width=device-width, initial-scale=1" />
</svelte:head>

<div class="shell">
  <header class="topbar">
    <div class="brand"><span class="mark">F</span><div><strong>Flow Studio</strong><small>Local .nflow editor</small></div></div>
    {#if project}<div class="file-label" title={activeFile}>{activeFile}</div>{/if}
    <div class="toolbar">
      <button class="secondary" type="button" disabled={!project || !isDirty || !!busy} on:click={saveOnly}>{busy === 'save' ? 'Saving…' : 'Save'}</button>
      <button class="secondary" type="button" disabled={!project || !!busy} on:click={() => perform('validate')}>{busy === 'validate' ? 'Checking…' : 'Validate'}</button>
      <button class="secondary" type="button" disabled={!project || !!busy} on:click={showExplain}>{busy === 'explain' ? 'Explaining…' : 'Explain'}</button>
      <button class="primary" type="button" disabled={!project || !!busy} on:click={() => perform('run')}>{busy === 'run' ? 'Running…' : 'Run flow'}</button>
    </div>
  </header>
  <main>
    {#if errorMessage}
      <div class="notice error" role="alert"><strong>Studio action failed</strong><span>{errorMessage}</span></div>
    {/if}
    {#if project?.index_error}
      <div class="notice warning" role="status"><strong>Include index warning</strong><span>{project.index_error}</span><span>The entrypoint stays editable so you can fix the include.</span></div>
    {/if}
    {#if statusMessage}
      <div class="notice success" role="status">{statusMessage}{#if isDirty} · Unsaved changes{/if}</div>
    {/if}
    {#if !project && !errorMessage}
      <div class="loading">Loading Flow project…</div>
    {:else if project}
      <div class="workspace">
        <aside class="panel tree-panel">
          <div class="panel-heading"><div><strong>{view === 'explain' ? 'Entrypoint map' : 'Project files'}</strong><span>Flow compiler include index</span></div></div>
          <nav aria-label="Flow project files"><FileTree nodes={[view === 'explain' && explanation ? explanation.map : project.tree]} active={activeFile} onSelect={openFile} /></nav>
        </aside>
        <section class="panel editor-panel">
          <div class="panel-heading"><div><strong>Flow source</strong><span>Edit the saved .nflow file</span></div><span class:dirty={isDirty} class="save-state">{isDirty ? 'Unsaved' : 'Saved'}</span></div>
          <textarea aria-label="Flow source editor" spellcheck="false" bind:value={source}></textarea>
        </section>
        <section class="panel result-panel">
          {#if view === 'explain' && explanation}
            <div class="panel-heading"><div><strong>Explain</strong><span>Flow's native AST-derived plans; action argument values are omitted</span></div></div>
            <div class="plan-list">
              <p class="entrypoint-label">Entrypoint: <code>{explanation.entrypoint}</code></p>
              {#each explanation.modules as module (module.file)}
                <section class="plan-card">
                  <h2>{module.file}</h2>
                  <pre class="output">{pretty(module.plan)}</pre>
                </section>
              {/each}
            </div>
          {:else}
            <div class="panel-heading"><div><strong>Output</strong><span>Validation diagnostics and run results</span></div></div>
            <pre class="output">{output || 'Use Validate to check the Flow project, or Run flow to execute it locally.'}</pre>
            <label class="payload-label" for="payload">Run payload (JSON object)</label>
            <textarea id="payload" class="payload" spellcheck="false" bind:value={payload}></textarea>
            <p class="security-note">Run executes this flow on your machine with the same permissions as nexssp.</p>
          {/if}
        </section>
      </div>
    {/if}
  </main>
  <footer>Flow/NFlow remains the source of truth · Studio is available only on this computer</footer>
</div>

<style>
  :global(*) { box-sizing: border-box; }
  :global(body) { margin: 0; background: #0b1120; color: #e5eaf4; font: 14px/1.5 Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
  :global(button), :global(textarea) { font: inherit; }
  .shell { min-height: 100vh; display: flex; flex-direction: column; }
  .topbar { min-height: 76px; padding: 14px 24px; display: flex; align-items: center; gap: 24px; border-bottom: 1px solid #253148; background: #10192a; }
  .brand { display: flex; align-items: center; gap: 11px; min-width: 190px; }
  .brand strong, .brand small { display: block; }
  .brand strong { font-size: 15px; letter-spacing: .01em; }
  .brand small { color: #8997ae; font-size: 11px; margin-top: 1px; }
  .mark { width: 34px; height: 34px; display: grid; place-items: center; border-radius: 10px; color: #d7eaff; background: #174877; font-weight: 750; font-size: 17px; }
  .file-label { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #a9b7cd; font: 12px ui-monospace, SFMono-Regular, Menlo, monospace; }
  .toolbar { margin-left: auto; display: flex; gap: 8px; }
  button { border: 1px solid #33435d; border-radius: 7px; padding: 8px 13px; cursor: pointer; color: #dae4f3; background: #1a263a; font-weight: 600; }
  button:hover:not(:disabled) { filter: brightness(1.16); }
  button:disabled { opacity: .48; cursor: not-allowed; }
  button.primary { border-color: #2878c2; color: white; background: #1769ad; }
  main { width: 100%; max-width: 1800px; margin: 0 auto; padding: 20px 24px; flex: 1; }
  .workspace { min-height: calc(100vh - 160px); display: grid; grid-template-columns: minmax(180px, .55fr) minmax(300px, 1.2fr) minmax(300px, .9fr); gap: 14px; }
  .panel { min-width: 0; display: flex; flex-direction: column; border: 1px solid #29364b; border-radius: 10px; overflow: hidden; background: #111a2b; }
  .panel-heading { min-height: 58px; display: flex; align-items: center; justify-content: space-between; padding: 10px 15px; border-bottom: 1px solid #29364b; }
  .panel-heading strong, .panel-heading span { display: block; }
  .panel-heading strong { font-size: 13px; }
  .panel-heading span { color: #8796ac; font-size: 11px; margin-top: 2px; }
  .panel-heading .save-state { margin: 0; padding: 3px 7px; border-radius: 5px; color: #88cfac; background: #16372d; }
  .panel-heading .save-state.dirty { color: #eac37d; background: #40351e; }
  .tree-panel nav { overflow: auto; padding: 10px 8px; }
  textarea { width: 100%; min-height: 300px; flex: 1; resize: vertical; border: 0; outline: none; padding: 16px; color: #dce8f7; background: #0d1524; font: 13px/1.65 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; tab-size: 2; }
  textarea:focus { box-shadow: inset 0 0 0 1px #2a75b1; }
  .result-panel { padding-bottom: 14px; }
  .output { min-height: 180px; max-height: 55vh; overflow: auto; flex: 1; margin: 0; padding: 15px; white-space: pre-wrap; overflow-wrap: anywhere; color: #c3d2e4; background: #0b1321; font: 12px/1.65 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
  .payload-label { margin: 13px 14px 6px; color: #b2bfd1; font-size: 12px; font-weight: 600; }
  textarea.payload { min-height: 75px; max-height: 160px; flex: initial; margin: 0 14px; width: calc(100% - 28px); border: 1px solid #29364b; border-radius: 6px; padding: 9px; }
  .security-note { margin: 8px 14px 0; color: #8796ac; font-size: 11px; }
  .plan-list { min-height: 0; overflow: auto; padding: 13px; }
  .entrypoint-label { margin: 0 0 12px; color: #b2bfd1; font-size: 12px; }
  code { color: #b7d9ff; }
  .plan-card { margin-bottom: 12px; border: 1px solid #29364b; border-radius: 7px; overflow: hidden; }
  .plan-card h2 { margin: 0; padding: 9px 11px; color: #dbeaff; background: #172338; font-size: 12px; }
  .plan-card .output { min-height: 90px; max-height: 35vh; }
  .notice { display: flex; gap: 10px; align-items: baseline; margin-bottom: 14px; padding: 10px 13px; border-radius: 7px; }
  .notice.error { border: 1px solid #743b42; color: #ffd8d9; background: #321d27; }
  .notice.warning { display: block; border: 1px solid #725c35; color: #f0d8a6; background: #302a1d; }
  .notice.warning span { display: block; overflow-wrap: anywhere; }
  .notice.error span { overflow-wrap: anywhere; }
  .notice.success { color: #ace4c4; background: #163126; }
  .loading { padding: 36px; color: #a6b5ca; text-align: center; }
  footer { padding: 14px 24px; border-top: 1px solid #253148; color: #8190a6; font-size: 11px; text-align: center; }
  @media (max-width: 1000px) { .topbar { padding: 12px 14px; gap: 12px; flex-wrap: wrap; } .brand { min-width: 150px; } .file-label { order: 3; width: 100%; } .toolbar { gap: 6px; } button { padding: 7px 9px; font-size: 12px; } main { padding: 12px; } .workspace { grid-template-columns: minmax(180px, .45fr) 1fr; min-height: auto; } .result-panel { grid-column: 1 / -1; min-height: 42vh; } .editor-panel { min-height: 54vh; } footer { padding: 12px; } }
  @media (max-width: 650px) { .workspace { grid-template-columns: 1fr; } .tree-panel { max-height: 220px; } .editor-panel, .result-panel { min-height: 42vh; grid-column: auto; } }
</style>
