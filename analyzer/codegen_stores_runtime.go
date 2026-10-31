package analyzer

// Verbatim TS runtime support for generated stores. Written once, then never
// overwritten so users can extend it. Each file's header documents this.
//
// The `extends object` constraint (instead of `extends Record<string, unknown>`)
// is deliberate: named DTO interfaces like `Project`, `User`, `ControlReq`
// do not carry a `[key: string]: unknown` index signature, so they do not
// satisfy `Record<string, unknown>`. `extends object` still guarantees the
// value is a non-primitive with property access, without forcing every DTO
// to declare an index signature.

const runtimeEntityStoreTS = `// ⚠️  Write-once support code for generated entity stores.
// You may edit this file — subsequent 'nexssp sdk' runs will not overwrite it.
// Delete it to force regeneration on the next run.

import type { ZodType } from 'zod';

export interface EntityApi<T, CreateReq, UpdateReq, GetReq> {
  list?:   (req?: unknown) => Promise<T[] | { items?: T[] | null; total?: number | null }>;
  get?:    (req: GetReq)   => Promise<T>;
  create?: (req: CreateReq) => Promise<T>;
  update?: (req: UpdateReq) => Promise<T>;
  remove?: (req: GetReq)   => Promise<unknown>;
}

export interface EntityConfig<T, CreateReq, UpdateReq, GetReq> {
  resource: string;
  idField:  keyof T & string;
  api:      EntityApi<T, CreateReq, UpdateReq, GetReq>;
  schemas?: {
    create?: ZodType<CreateReq>;
    update?: ZodType<UpdateReq>;
    get?:    ZodType<GetReq>;
  };
}

export function createEntityStore<
  T extends object,
  CreateReq = Partial<T>,
  UpdateReq = Partial<T>,
  GetReq    = { id: string },
>(cfg: EntityConfig<T, CreateReq, UpdateReq, GetReq>) {
  let items   = $state<T[]>([]);
  let byId    = $state<Record<string, T>>({});
  let loading = $state(false);
  let error   = $state<string | null>(null);
  let total   = $state(0);

  function asMessage(e: unknown): string {
    return e instanceof Error ? e.message : String(e);
  }

  async function list(params?: unknown): Promise<T[]> {
    if (!cfg.api.list) return items;
    loading = true; error = null;
    try {
      const res = await cfg.api.list(params);
      const arr = Array.isArray(res) ? res : (res?.items ?? []);
      const next: Record<string, T> = {};
      for (const it of arr) next[String(it[cfg.idField])] = it;
      items = arr;
      byId  = next;
      total = Array.isArray(res) ? arr.length : (res?.total ?? arr.length);
      return items;
    } catch (e) { error = asMessage(e); throw e; } finally { loading = false; }
  }

  async function get(req: GetReq): Promise<T> {
    if (cfg.schemas?.get) cfg.schemas.get.parse(req);
    if (!cfg.api.get) throw new Error(cfg.resource + ': get() not bound');
    loading = true; error = null;
    try {
      const it = await cfg.api.get(req);
      const id = String(it[cfg.idField]);
      byId[id] = it;
      const idx = items.findIndex((x) => String(x[cfg.idField]) === id);
      if (idx >= 0) items[idx] = it; else items = items.concat([it]);
      return it;
    } catch (e) { error = asMessage(e); throw e; } finally { loading = false; }
  }

  async function create(req: CreateReq): Promise<T> {
    if (cfg.schemas?.create) cfg.schemas.create.parse(req);
    if (!cfg.api.create) throw new Error(cfg.resource + ': create() not bound');
    loading = true; error = null;
    try {
      const it = await cfg.api.create(req);
      byId[String(it[cfg.idField])] = it;
      items = items.concat([it]);
      return it;
    } catch (e) { error = asMessage(e); throw e; } finally { loading = false; }
  }

  async function update(id: string, req: UpdateReq): Promise<T> {
    if (cfg.schemas?.update) cfg.schemas.update.parse(req);
    if (!cfg.api.update) throw new Error(cfg.resource + ': update() not bound');
    loading = true; error = null;
    try {
      const it = await cfg.api.update(req);
      const key = String(it[cfg.idField] ?? id);
      byId[key] = it;
      const idx = items.findIndex((x) => String(x[cfg.idField]) === key);
      if (idx >= 0) items[idx] = it; else items = items.concat([it]);
      return it;
    } catch (e) { error = asMessage(e); throw e; } finally { loading = false; }
  }

  async function remove(req: GetReq): Promise<void> {
    if (cfg.schemas?.get) cfg.schemas.get.parse(req);
    if (!cfg.api.remove) throw new Error(cfg.resource + ': remove() not bound');
    loading = true; error = null;
    try {
      await cfg.api.remove(req);
      const r = req as Record<string, unknown>;
      const id = String(r[cfg.idField] ?? r.id);
      const next: Record<string, T> = {};
      for (const k in byId) if (k !== id) next[k] = byId[k];
      byId = next;
      items = items.filter((x) => String(x[cfg.idField]) !== id);
    } catch (e) { error = asMessage(e); throw e; } finally { loading = false; }
  }

  return {
    get items()   { return items; },
    get byId()    { return byId; },
    get loading() { return loading; },
    get error()   { return error; },
    get total()   { return total; },
    list, get, create, update, remove,
  };
}
`

const runtimeFormStoreTS = `// ⚠️  Write-once support code for generated form stores.
// You may edit this file — subsequent 'nexssp sdk' runs will not overwrite it.
// Delete it to force regeneration on the next run.

import type { ZodType } from 'zod';

export interface FormConfig<Req extends object, Res> {
  submit:  (req: Req) => Promise<Res>;
  schema:  ZodType<Req>;
  initial: Req;
}

export function createFormStore<Req extends object, Res>(cfg: FormConfig<Req, Res>) {
  let values  = $state<Req>({ ...cfg.initial } as Req);
  let errors  = $state<Record<string, string>>({});
  let loading = $state(false);
  let result  = $state<Res | null>(null);
  let dirty   = $state(false);

  function validate(): boolean {
    const res = cfg.schema.safeParse(values);
    if (res.success) { errors = {}; return true; }
    const fieldErrs: Record<string, string> = {};
    for (const issue of res.error.issues) {
      const k = issue.path.join('.') || '_form';
      if (!fieldErrs[k]) fieldErrs[k] = issue.message;
    }
    errors = fieldErrs;
    return false;
  }

  async function submit(): Promise<Res | null> {
    dirty = true;
    if (!validate()) return null;
    loading = true;
    try {
      result = await cfg.submit(values);
      return result;
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Request failed';
      errors = { _form: msg };
      return null;
    } finally { loading = false; }
  }

  function reset(): void {
    values = { ...cfg.initial } as Req;
    errors = {};
    result = null;
    dirty  = false;
  }

  return {
    get values()  { return values; },
    set values(v) { values = v; if (dirty) validate(); },
    get errors()  { return errors; },
    get loading() { return loading; },
    get result()  { return result; },
    get dirty()   { return dirty; },
    submit, reset, validate,
  };
}
`

const runtimeStreamStoreTS = `// ⚠️  Write-once support code for generated stream stores.
// You may edit this file — subsequent 'nexssp sdk' runs will not overwrite it.
// Delete it to force regeneration on the next run.

export interface StreamConfig<E> {
  url:      string;
  channel?: string;
  cap?:     number;
  parse?:   (raw: string) => E;
}

export function createStreamStore<E = unknown>(cfg: StreamConfig<E>) {
  let events    = $state<E[]>([]);
  let connected = $state(false);
  let error     = $state<string | null>(null);
  let paused    = $state(false);
  let total     = $state(0);

  let es: EventSource | null = null;
  const cap = cfg.cap ?? 500;
  const parseFn: (raw: string) => E =
    cfg.parse ?? ((raw: string) => JSON.parse(raw) as E);

  function push(e: E): void {
    const next = events.length >= cap
      ? events.slice(events.length - cap + 1)
      : events.slice();
    next.push(e);
    events = next;
    total += 1;
  }

  function connect(): void {
    if (es || paused || typeof window === 'undefined') return;
    es = new EventSource(cfg.url);
    es.onopen  = () => { connected = true; error = null; };
    es.onerror = () => { connected = false; };
    const handler = (msg: MessageEvent) => {
      if (paused) return;
      try { push(parseFn(msg.data)); }
      catch (e) { error = e instanceof Error ? e.message : 'parse failed'; }
    };
    if (cfg.channel) es.addEventListener(cfg.channel, handler as EventListener);
    else es.onmessage = handler;
  }

  function disconnect(): void {
    if (!es) return;
    es.close();
    es = null;
    connected = false;
  }

  function pause(): void { paused = true; }
  function resume(): void { paused = false; connect(); }
  function clear(): void { events = []; }

  connect();

  return {
    get events()    { return events; },
    get connected() { return connected; },
    get error()     { return error; },
    get paused()    { return paused; },
    get total()     { return total; },
    connect, disconnect, pause, resume, clear,
  };
}
`

const runtimeIndexTS = `// ⚠️  Write-once barrel for the generated store runtime.
// You may edit this file — subsequent 'nexssp sdk' runs will not overwrite it.

export { createEntityStore } from './entity.svelte';
export { createFormStore }   from './form.svelte';
export { createStreamStore } from './stream.svelte';

export type { EntityApi, EntityConfig } from './entity.svelte';
export type { FormConfig }              from './form.svelte';
export type { StreamConfig }            from './stream.svelte';
`
