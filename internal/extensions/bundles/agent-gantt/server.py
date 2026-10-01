#!/usr/bin/env python3
"""Agent Chat/Gantt v2. Stdlib only; one canonical payload per MCP result."""
import base64
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import sys
from datetime import date, datetime, timezone
from contextlib import closing

VERSION = '2.0.0'
BUDGET = 16384
STATUSES = ['todo', 'in_progress', 'blocked', 'done']
FIELDS = {'title', 'summary', 'parent_task_id', 'description', 'status', 'start_date', 'due_date', 'assignee', 'progress'}


def dump(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':'))


def integer(a, key, default=None, low=0, high=2**63-1):
    v = a.get(key, default)
    if type(v) is not int or not low <= v <= high:
        raise ValueError(f'{key}: integer required in [{low},{high}]')
    return v


def string(a, key, required=False):
    v = a.get(key, '')
    if not isinstance(v, str) or (required and not v.strip()):
        raise ValueError(f'{key}: nonempty string required' if required else f'{key}: string required')
    return v


def now():
    return datetime.now(timezone.utc).isoformat()


class Server:
    def __init__(self, kind, path, transport='text', profile='full'):
        if kind not in ('chat', 'gantt') or transport not in ('text', 'structured'):
            raise ValueError('invalid server kind or transport')
        if profile not in ('full','worker'):
            raise ValueError('profile must be full or worker')
        self.profile = profile
        self.kind, self.path, self.transport = kind, Path(path), transport
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with closing(self.connect()) as c, c:
            c.execute('BEGIN IMMEDIATE')
            c.execute('CREATE TABLE IF NOT EXISTS mcp_requests (key TEXT PRIMARY KEY, request TEXT NOT NULL, response TEXT NOT NULL)')
            if kind == 'chat':
                c.execute('CREATE TABLE IF NOT EXISTS messages (id INTEGER PRIMARY KEY AUTOINCREMENT, room TEXT NOT NULL, author TEXT NOT NULL, body TEXT NOT NULL, recipients TEXT, metadata TEXT, created_at TEXT NOT NULL)')
                c.execute('CREATE INDEX IF NOT EXISTS messages_room_id ON messages(room,id)')
            else:
                c.execute('CREATE TABLE IF NOT EXISTS projects (id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,description TEXT,created_at TEXT NOT NULL)')
                c.execute("CREATE TABLE IF NOT EXISTS tasks (id INTEGER PRIMARY KEY AUTOINCREMENT,project_id INTEGER NOT NULL REFERENCES projects(id),title TEXT NOT NULL,description TEXT,status TEXT NOT NULL DEFAULT 'todo',start_date TEXT,due_date TEXT,assignee TEXT,progress INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL)")
                c.execute('CREATE TABLE IF NOT EXISTS dependencies (task_id INTEGER NOT NULL REFERENCES tasks(id),depends_on_id INTEGER NOT NULL REFERENCES tasks(id),PRIMARY KEY(task_id,depends_on_id))')
                c.execute('CREATE TABLE IF NOT EXISTS mcp_events (revision INTEGER PRIMARY KEY AUTOINCREMENT,project_id INTEGER NOT NULL,task_id INTEGER NOT NULL,payload TEXT NOT NULL)')
                c.execute('CREATE INDEX IF NOT EXISTS mcp_events_project ON mcp_events(project_id,revision)')
                c.execute('CREATE INDEX IF NOT EXISTS mcp_events_task ON mcp_events(task_id,revision)')
                for table, additions in {
                    'tasks': {'summary': "TEXT NOT NULL DEFAULT ''", 'parent_task_id': 'INTEGER REFERENCES tasks(id)'},
                    'mcp_events': {'actor': "TEXT NOT NULL DEFAULT ''", 'reason': "TEXT NOT NULL DEFAULT ''", 'recorded_at': "TEXT NOT NULL DEFAULT ''", 'operation': "TEXT NOT NULL DEFAULT ''"},
                }.items():
                    existing = {r['name'] for r in c.execute(f'PRAGMA table_info({table})')}
                    for column, declaration in additions.items():
                        if column not in existing:
                            c.execute(f'ALTER TABLE {table} ADD COLUMN {column} {declaration}')
                c.execute('CREATE TABLE IF NOT EXISTS mcp_claims (task_id INTEGER PRIMARY KEY REFERENCES tasks(id),owner TEXT NOT NULL)')
                # Additive, idempotent migration of existing tasks, under a writer lock.
                for row in c.execute('SELECT id FROM tasks WHERE id NOT IN (SELECT task_id FROM mcp_events)').fetchall():
                    self.record(c, row['id'])

    def connect(self):
        c = sqlite3.connect(self.path, timeout=30)
        c.row_factory = sqlite3.Row
        c.execute('PRAGMA foreign_keys=ON')
        return c

    def result(self, value):
        if self.transport == 'structured':
            return {'content': [], 'structuredContent': value}
        return {'content': [{'type': 'text', 'text': dump(value)}]}

    def record(self, c, task_id, audit=None, operation='migration'):
        audit = audit or {}
        task = dict(self.required(c, 'tasks', task_id))
        task['depends_on'] = [r[0] for r in c.execute('SELECT depends_on_id FROM dependencies WHERE task_id=? ORDER BY depends_on_id', (task_id,))]
        cur = c.execute('INSERT INTO mcp_events(project_id,task_id,payload,actor,reason,recorded_at,operation) VALUES(?,?,?,?,?,?,?)', (task['project_id'], task_id, dump(task), string(audit,'actor'), string(audit,'reason'), now(), operation))
        return cur.lastrowid

    @staticmethod
    def required(c, table, ident):
        row = c.execute(f'SELECT * FROM {table} WHERE id=?', (ident,)).fetchone()
        if row is None:
            raise ValueError(f'{table}: id not found')
        return row

    def call(self, name, a):
        if not isinstance(a, dict):
            raise ValueError('arguments must be an object')
        schemas = {t['name']: t['inputSchema'] for t in self.tools()}
        if name not in schemas:
            raise ValueError('unknown tool')
        if set(a) - set(schemas[name]['properties']):
            raise ValueError('unknown arguments')
        for key in schemas[name].get('required', []):
            if key not in a:
                raise ValueError(f'{key}: required')
        writes = {'post_message', 'create_project', 'create_task', 'update_task', 'add_dependency', 'batch', 'coordinate'}
        c = self.connect()
        try:
            c.execute('BEGIN IMMEDIATE' if name in writes else 'BEGIN')
            key = string(a, 'idempotency_key')
            request = dump({'tool': name, 'arguments': {k: v for k, v in sorted(a.items()) if k != 'idempotency_key'}})
            if key:
                saved = c.execute('SELECT * FROM mcp_requests WHERE key=?', (key,)).fetchone()
                if saved:
                    if saved['request'] != request:
                        raise ValueError('idempotency_key reused with different arguments')
                    return json.loads(saved['response'])
            if name in writes:
                out = self.write(c, name, a)
            else:
                out = self.read(c, name, a)
            if key:
                c.execute('INSERT INTO mcp_requests VALUES(?,?,?)', (key, request, dump(out)))
            c.commit()
            return out
        finally:
            c.close()

    def write(self, c, name, a):
        if name == 'coordinate':
            action = string(a,'action',True)
            if action not in ('claim','release','complete','block','resume','report'):
                raise ValueError('invalid coordination action')
            if action != 'report' and 'progress' in a:
                raise ValueError('progress is only accepted by report')
            task_id = integer(a,'task_id',low=1)
            actor = string(a,'actor',True)
            string(a,'reason',True)
            task = self.required(c,'tasks',task_id)
            revision = c.execute('SELECT MAX(revision) FROM mcp_events WHERE task_id=?',(task_id,)).fetchone()[0]
            if integer(a,'expected_revision',low=1) != revision:
                raise ValueError('stale task revision; read changes before retrying')
            claim = c.execute('SELECT owner FROM mcp_claims WHERE task_id=?',(task_id,)).fetchone()
            blockers = c.execute("SELECT t.id FROM dependencies d JOIN tasks t ON t.id=d.depends_on_id WHERE d.task_id=? AND t.status!='done'",(task_id,)).fetchall()
            if action == 'claim':
                if claim or task['status'] != 'todo' or task['assignee'] not in ('',None,actor) or blockers:
                    raise ValueError('task unavailable, assigned elsewhere, or dependencies incomplete')
                c.execute('INSERT INTO mcp_claims VALUES(?,?)',(task_id,actor))
                c.execute("UPDATE tasks SET status='in_progress',assignee=? WHERE id=?",(actor,task_id))
            else:
                if not claim or claim['owner'] != actor:
                    raise ValueError('only the claiming agent can release or complete this task')
                if action in ('complete','resume') and blockers:
                    raise ValueError('dependencies incomplete')
                if action == 'complete' and c.execute("SELECT id FROM tasks WHERE parent_task_id=? AND status!='done'",(task_id,)).fetchone():
                    raise ValueError('subtasks incomplete')
                if action in ('release','complete'):
                    c.execute('DELETE FROM mcp_claims WHERE task_id=?',(task_id,))
                if action == 'report':
                    progress = integer(a,'progress',low=0,high=99)
                    c.execute('UPDATE tasks SET progress=? WHERE id=?',(progress,task_id))
                elif action in ('block','resume'):
                    c.execute('UPDATE tasks SET status=? WHERE id=?',('blocked' if action=='block' else 'in_progress',task_id))
                elif action == 'complete':
                    c.execute("UPDATE tasks SET status='done',progress=100 WHERE id=?",(task_id,))
                else:
                    c.execute("UPDATE tasks SET status='todo',assignee='' WHERE id=?",(task_id,))
            return {'task_id':task_id,'revision':self.record(c,task_id,a,action)}
        if name == 'post_message':
            room, author, body = (string(a, k, True) for k in ('room', 'author', 'message'))
            recipients, metadata = a.get('recipients', []), a.get('metadata', {})
            if not isinstance(recipients, list) or any(not isinstance(v, str) or not v for v in recipients) or not isinstance(metadata, dict):
                raise ValueError('invalid recipients or metadata')
            cur = c.execute('INSERT INTO messages(room,author,body,recipients,metadata,created_at) VALUES(?,?,?,?,?,?)', (room, author, body, dump(recipients), dump(metadata), now()))
            return {'id': cur.lastrowid}
        if name == 'create_project':
            cur = c.execute('INSERT INTO projects(name,description,created_at) VALUES(?,?,?)', (string(a, 'name', True), string(a, 'description'), now()))
            return {'project_id': cur.lastrowid}
        if name == 'batch':
            ops = a.get('operations')
            if not isinstance(ops, list) or not 1 <= len(ops) <= 50:
                raise ValueError('operations: 1..50 entries required')
            refs, results = {}, []
            for op in ops:
                if not isinstance(op, dict) or set(op) - {'tool', 'arguments', 'ref'} or op.get('tool') not in ('create_task', 'update_task', 'add_dependency'):
                    raise ValueError('invalid batch operation')
                args = op.get('arguments', {})
                if not isinstance(args, dict):
                    raise ValueError('operation arguments must be an object')
                args = dict(args)
                allowed = next(t['inputSchema']['properties'] for t in self.tools() if t['name'] == op['tool'])
                if set(args) - (set(allowed) - {'idempotency_key'}):
                    raise ValueError('unknown batch arguments')
                for field in ('task_id', 'depends_on_id', 'parent_task_id'):
                    if isinstance(args.get(field), str):
                        ref = args[field]
                        if not ref.startswith('$') or ref[1:] not in refs:
                            raise ValueError('unknown local reference')
                        args[field] = refs[ref[1:]]
                result = self.write(c, op['tool'], args)
                if 'ref' in op:
                    ref = string(op, 'ref', True)
                    if ref in refs or op['tool'] != 'create_task':
                        raise ValueError('ref must uniquely name a newly created task')
                    refs[ref] = result['task_id']
                results.append(result)
            return {'results': results}
        if name == 'add_dependency':
            task_id, predecessor = integer(a, 'task_id', low=1), integer(a, 'depends_on_id', low=1)
            t, p = self.required(c, 'tasks', task_id), self.required(c, 'tasks', predecessor)
            self.check_claim(c,task_id,a)
            if task_id == predecessor or t['project_id'] != p['project_id']:
                raise ValueError('invalid dependency')
            if c.execute('WITH RECURSIVE ancestors(id) AS (SELECT depends_on_id FROM dependencies WHERE task_id=? UNION SELECT d.depends_on_id FROM dependencies d JOIN ancestors a ON d.task_id=a.id) SELECT 1 FROM ancestors WHERE id=?', (predecessor, task_id)).fetchone():
                raise ValueError('dependency would create a cycle')
            cur = c.execute('INSERT OR IGNORE INTO dependencies VALUES(?,?)', (task_id, predecessor))
            self.check_completion_graph(c)
            if c.execute('SELECT 1 FROM mcp_claims WHERE task_id=?',(task_id,)).fetchone() and p['status'] != 'done':
                raise ValueError('cannot add unfinished predecessor to active/completed task')
            return {'task_id': task_id, 'revision': self.record(c, task_id,a,name) if cur.rowcount else self.revision(c)}
        if name == 'create_task':
            project = integer(a, 'project_id', low=1)
            self.required(c, 'projects', project)
            values = dict(title=string(a, 'title', True), summary='', parent_task_id=None, description='', status='todo', start_date=None, due_date=None, assignee='', progress=0)
        else:
            task_id = integer(a, 'task_id', low=1)
            values = dict(self.required(c, 'tasks', task_id))
            self.check_claim(c,task_id,a)
            if c.execute('SELECT 1 FROM mcp_claims WHERE task_id=?',(task_id,)).fetchone() and (set(a) & {'status','assignee'}):
                raise ValueError('use coordinate release/complete for claimed task transitions')
            if not FIELDS.intersection(a):
                raise ValueError('provide task fields')
        values.update({k: a[k] for k in FIELDS.intersection(a)})
        for key in ('title', 'description', 'assignee'):
            string(values, key, key == 'title')
        summary = string(values,'summary')
        if len(summary)>80 or '\n' in summary or '\r' in summary:
            raise ValueError('summary: single line, at most 80 characters')
        parent = values.get('parent_task_id')
        if parent is not None:
            integer(values,'parent_task_id',low=1)
            parent_row = self.required(c,'tasks',parent)
            project_id = project if name=='create_task' else values['project_id']
            if parent_row['project_id'] != project_id:
                raise ValueError('parent must belong to the same project')
            if parent_row['status']=='done' and values['status']!='done':
                raise ValueError('cannot attach unfinished work to a completed parent')
            if name != 'create_task' and (parent == task_id or c.execute('WITH RECURSIVE ancestors(id) AS (SELECT parent_task_id FROM tasks WHERE id=? UNION SELECT t.parent_task_id FROM tasks t JOIN ancestors a ON t.id=a.id WHERE t.parent_task_id IS NOT NULL) SELECT 1 FROM ancestors WHERE id=?',(parent,task_id)).fetchone()):
                raise ValueError('parent cycle')
        if values['status'] not in STATUSES:
            raise ValueError('invalid status')
        integer(values, 'progress', low=0, high=100)
        for key in ('start_date', 'due_date'):
            v = values[key]
            if v is not None and (not isinstance(v, str) or date.fromisoformat(v).isoformat() != v):
                raise ValueError('invalid ISO date')
        if values['start_date'] and values['due_date'] and values['due_date'] < values['start_date']:
            raise ValueError('due_date precedes start_date')
        fields = sorted(FIELDS)
        if name == 'create_task':
            cur = c.execute(f'INSERT INTO tasks(project_id,created_at,{",".join(fields)}) VALUES({",".join("?" for _ in range(len(fields)+2))})', (project, now(), *(values[k] for k in fields)))
            task_id = cur.lastrowid
        else:
            c.execute(f'UPDATE tasks SET {",".join(k+"=?" for k in fields)} WHERE id=?', (*(values[k] for k in fields), task_id))
        self.check_completion_graph(c)
        return {'task_id': task_id, 'revision': self.record(c, task_id,a,name)}

    @staticmethod
    def check_completion_graph(c):
        # A parent finishes after its children, even though it may start first.
        # Combining that rule with explicit predecessors must not create deadlock.
        if c.execute('''WITH RECURSIVE edges(task_id,predecessor) AS (
            SELECT task_id,depends_on_id FROM dependencies UNION
            SELECT parent_task_id,id FROM tasks WHERE parent_task_id IS NOT NULL
        ), reach(source,target) AS (
            SELECT task_id,predecessor FROM edges UNION
            SELECT r.source,e.predecessor FROM reach r JOIN edges e ON e.task_id=r.target
        ) SELECT 1 FROM reach WHERE source=target LIMIT 1''').fetchone():
            raise ValueError('dependency and subtasks would create a completion deadlock')

    @staticmethod
    def check_claim(c,task_id,a):
        claim = c.execute('SELECT owner FROM mcp_claims WHERE task_id=?',(task_id,)).fetchone()
        if claim and string(a,'actor') != claim['owner']:
            raise ValueError('task is claimed by another agent')

    @staticmethod
    def revision(c):
        return c.execute('SELECT COALESCE(MAX(revision),0) FROM mcp_events').fetchone()[0]

    @staticmethod
    def cursor(scope, last, upper):
        return base64.urlsafe_b64encode(dump([scope, last, upper]).encode()).decode().rstrip('=')

    def bounds(self, name, a, upper):
        scope = hashlib.sha256(dump([name, sorted((k, v) for k, v in a.items() if k not in ('cursor', 'limit'))]).encode()).hexdigest()[:16]
        last = integer(a, 'after_id', 0) if name == 'list_messages' else (integer(a, 'after_revision', 0) if name == 'changes' else 0)
        if a.get('cursor'):
            try:
                token = string(a, 'cursor')
                s, last, bound = json.loads(base64.urlsafe_b64decode(token + '=' * (-len(token) % 4)))
                if s != scope or type(last) is not int or type(bound) is not int or last < 0 or not 0 <= bound <= upper or (name != 'project_view' and last > bound):
                    raise ValueError()
                upper = bound
            except Exception as e:
                raise ValueError('invalid cursor or changed filters') from e
        elif last > upper:
            raise ValueError('cursor ahead of database')
        return scope, last, upper

    def page(self, name, a, rows, upper, key, convert, envelope=None):
        scope, last, upper = self.bounds(name, a, upper)
        limit = integer(a, 'limit', 25, 1, 100)
        out = dict(envelope or {}, items=[], next_cursor=None, has_more=False)
        if name == 'changes':
            out['through_revision'] = upper
        if name == 'list_messages':
            out['through_id'] = upper
        for row in rows(last, upper):
            if len(out['items']) >= limit:
                out['has_more'] = True
                break
            item = convert(row)
            trial = dict(out, items=out['items'] + [item])
            if len(dump(self.result(trial)).encode()) > BUDGET - 700:
                if out['items']:
                    out['has_more'] = True
                    break
                item = {'id': row[key], 'omitted': 'oversized; use get_detail', 'detail': row.get('_detail', {'kind': 'message', 'id': row[key]})}
            out['items'].append(item)
            last = row[key]
        out['next_cursor'] = self.cursor(scope, last, upper) if out['has_more'] else None
        out['last_id' if name != 'changes' else 'last_revision'] = last
        return out

    def snapshots(self, c, project, upper):
        return [dict(json.loads(r['payload']), revision=r['revision']) for r in c.execute('SELECT e.* FROM mcp_events e JOIN (SELECT task_id,MAX(revision) r FROM mcp_events WHERE project_id=? AND revision<=? GROUP BY task_id) latest ON e.revision=latest.r ORDER BY e.task_id', (project, upper))]

    def read(self, c, name, a):
        if name == 'get_detail':
            kind, ident = string(a, 'kind', True), integer(a, 'id', low=1)
            if self.kind == 'chat' and kind == 'message':
                value = self.message(self.required(c, 'messages', ident), full=True)
            elif self.kind == 'gantt' and kind in ('task', 'event'):
                if kind == 'event':
                    row = c.execute('SELECT * FROM mcp_events WHERE revision=?', (ident,)).fetchone()
                else:
                    row = c.execute('SELECT * FROM mcp_events WHERE task_id=? ORDER BY revision DESC LIMIT 1', (ident,)).fetchone()
                if row is None:
                    raise ValueError('detail not found')
                value = dict(json.loads(row['payload']), revision=row['revision'])
                value['audit'] = {k:row[k] for k in ('actor','reason','recorded_at','operation')}
            elif self.kind == 'gantt' and kind == 'project':
                value = dict(self.required(c, 'projects', ident))
            else:
                raise ValueError('invalid detail kind')
            encoded = dump(value)
            offset, count = integer(a, 'offset', 0), integer(a, 'count', 2000, 1, 2000)
            if offset > len(encoded):
                raise ValueError('offset beyond detail')
            end = min(len(encoded), offset+count)
            out = {'json_segment': encoded[offset:end], 'next_offset': end if end < len(encoded) else None, 'total_chars': len(encoded)}
            if kind in ('task', 'event'):
                out['detail'] = {'kind': 'event', 'id': row['revision']}
            return out
        if name == 'list_messages':
            room = string(a, 'room', True)
            clauses, params = ['room=?'], [room]
            for field in ('author',):
                if field in a:
                    clauses.append(field+'=?'); params.append(string(a, field, True))
            if 'recipient' in a:
                clauses.append("(recipients IS NULL OR recipients='[]' OR EXISTS(SELECT 1 FROM json_each(messages.recipients) WHERE value=?))")
                params.append(string(a, 'recipient', True))
            if 'query' in a:
                clauses.append('instr(lower(body),lower(?))>0'); params.append(string(a, 'query', True))
            upper = c.execute('SELECT COALESCE(MAX(id),0) FROM messages').fetchone()[0]
            def rows(last, bound):
                return (dict(r) for r in c.execute('SELECT * FROM messages WHERE '+ ' AND '.join(clauses)+' AND id>? AND id<=? ORDER BY id', (*params,last,bound)))
            return self.page(name, a, rows, upper, 'id', self.message)
        if name == 'list_rooms':
            upper = c.execute('SELECT COALESCE(MAX(id),0) FROM messages').fetchone()[0]
            def rows(last,bound):
                return (dict(r) for r in c.execute('SELECT MIN(id) id,room,MAX(id) last_message_id FROM messages WHERE id<=? GROUP BY room HAVING MIN(id)>? ORDER BY MIN(id)',(bound,last)))
            return self.page(name,a,rows,upper,'id',lambda r:r)
        if name == 'list_projects':
            upper = c.execute('SELECT COALESCE(MAX(id),0) FROM projects').fetchone()[0]
            def rows(last, bound):
                return (dict(r, _detail={'kind':'project','id':r['id']}) for r in c.execute('SELECT id,name FROM projects WHERE id>? AND id<=? ORDER BY id',(last,bound)))
            return self.page(name,a,rows,upper,'id',lambda r:{'id':r['id'],'name':r['name']})
        project = integer(a, 'project_id', low=1)
        self.required(c, 'projects', project)
        upper = self.revision(c)
        if name == 'changes':
            def rows(last,bound):
                return (dict(r, _detail={'kind':'event','id':r['revision']}) for r in c.execute('SELECT * FROM mcp_events WHERE project_id=? AND revision>? AND revision<=? ORDER BY revision',(project,last,bound)))
            return self.page(name,a,rows,upper,'revision',lambda r:{'revision':r['revision'],'task':self.compact(dict(json.loads(r['payload']),revision=r['revision']))})
        _, _, upper = self.bounds(name,a,upper)
        tasks = self.snapshots(c, project, upper)
        by_id = {t['id']:t for t in tasks}
        if 'status' in a and a['status'] not in STATUSES:
            raise ValueError('invalid status')
        if 'assignee' in a:
            string(a,'assignee')
        if 'ready_only' in a and type(a['ready_only']) is not bool:
            raise ValueError('ready_only must be boolean')
        def rows(last,bound):
            def matches(t):
                if t['id']<=last or any(t[k]!=a[k] for k in ('status',) if k in a): return False
                if a.get('ready_only'):
                    return t['status']=='todo' and t.get('assignee','') in ('',None,a.get('assignee','')) and all(by_id[i]['status']=='done' for i in t['depends_on'])
                return 'assignee' not in a or t.get('assignee')==a['assignee']
            return (dict(t,_detail={'kind':'event','id':t['revision']}) for t in tasks if matches(t))
        def convert(t):
            out = self.compact(t)
            if t['depends_on']:
                out['predecessors'] = [{'id':i,'status':by_id[i]['status']} for i in t['depends_on']]
            return out
        # Task IDs and revision IDs are independent; pagination uses task IDs,
        # while the cursor upper bound is a revision snapshot.
        return self.page(name,a,rows,upper,'id',convert,{'revision':upper})

    @staticmethod
    def compact(t):
        out = {k:t[k] for k in ('id','status','assignee','progress','parent_task_id','start_date','due_date','depends_on','revision') if t.get(k) not in (None,'',[])}
        out['title'] = t.get('summary') or t['title']
        return out

    @staticmethod
    def message(row, full=False):
        out = {k:row[k] for k in ('id','author','body','created_at')}
        recipients, metadata = json.loads(row['recipients'] or '[]'), json.loads(row['metadata'] or '{}')
        if recipients: out['recipients'] = recipients
        if metadata: out['metadata'] = metadata
        if full: out['room'] = row['room']
        return out

    def tools(self):
        s, n = {'type':'string'}, {'type':'integer','minimum':1}
        paging = {'cursor':s,'limit':{'type':'integer','minimum':1,'maximum':100,'default':25}}
        idem = {'idempotency_key':s}
        task = {'title':s,'summary':{'type':'string','maxLength':80},'parent_task_id':{'type':['integer','null'],'minimum':1},'description':s,'assignee':s,'status':{'type':'string','enum':STATUSES},'progress':{'type':'integer','minimum':0,'maximum':100},'start_date':{'type':['string','null']},'due_date':{'type':['string','null']}}
        audit = {'actor':s,'reason':s}
        detail = ('get_detail','Retrieve complete JSON in segments; concatenate json_segment using next_offset. For stable task reads use kind=event and revision as id.',{'kind':{'type':'string','enum':['message'] if self.kind=='chat' else ['project','task','event']},'id':n,'offset':{'type':'integer','minimum':0},'count':{'type':'integer','minimum':1,'maximum':2000}},['kind','id'])
        if self.kind == 'chat':
            specs = [
                ('post_message','Persist a message. Reuse idempotency_key only for identical retries.',dict(room=s,author=s,message=s,recipients={'type':'array','items':s},metadata={'type':'object'},**idem),['room','author','message']),
                ('list_messages','Read chronological messages. Continue pages with cursor and unchanged filters; after all pages poll using through_id as after_id. Recipient includes public messages.',dict(room=s,after_id={'type':'integer','minimum':0},author=s,recipient=s,query=s,**paging),['room']),
                ('list_rooms','List rooms in creation order; continue with cursor.',paging,[]),detail]
        else:
            specs = [
                ('create_project','Create project.',dict(name=s,description=s,**idem),['name']),
                ('create_task','Create task. summary: few words for humans; description: agent detail. parent_task_id groups subtasks, not a dependency.',dict(project_id=n,**task,**audit,**idem),['project_id','title']),
                ('update_task','Update supplied fields; null clears a date. Claimed tasks require actor=owner.',dict(task_id=n,**task,**audit,**idem),['task_id']),
                ('add_dependency','Add predecessor; rejects cycles and cross-project edges.',dict(task_id=n,depends_on_id=n,**audit,**idem),['task_id','depends_on_id']),
                ('list_projects','List project IDs/names; detail on demand.',paging,[]),
                ('project_view','Paginated snapshot. Save revision. ready_only finds executable tasks; assignee includes unassigned when ready_only.',dict(project_id=n,status=task['status'],assignee=s,ready_only={'type':'boolean'},**paging),['project_id']),
                ('coordinate','Atomic task lifecycle with fresh revision. Claim checks assignment/dependencies. Owner can block/resume/report progress/release/complete; reason records evidence.',dict(action={'type':'string','enum':['claim','release','complete','block','resume','report']},task_id=n,expected_revision=n,progress={'type':'integer','minimum':0,'maximum':99},**audit,**idem),['action','task_id','expected_revision','actor','reason']),
                ('changes','Read unfiltered task changes after_revision. Apply by task ID; after all pages poll from through_revision. Get full task using event revision.',dict(project_id=n,after_revision={'type':'integer','minimum':0},**paging),['project_id']),
                ('batch','Atomic 1..50 task/dependency operations. Each has tool, arguments, optional ref on create_task. Use $ref in later task_id/depends_on_id.',{'operations':{'type':'array','minItems':1,'maxItems':50,'items':{'type':'object','required':['tool','arguments'],'properties':{'tool':{'type':'string','enum':['create_task','update_task','add_dependency']},'arguments':{'type':'object'},'ref':s},'additionalProperties':False}},**idem},['operations']),detail]
        if self.kind == 'gantt' and self.profile == 'worker':
            specs = [spec for spec in specs if spec[0] in ('project_view','changes','coordinate','get_detail')]
        return [{'name':name,'description':description,'inputSchema':{'type':'object','properties':props,'required':required,'additionalProperties':False}} for name,description,props,required in specs]

    def respond(self, msg):
        if not isinstance(msg,dict) or msg.get('jsonrpc')!='2.0' or not isinstance(msg.get('method'),str):
            return {'jsonrpc':'2.0','id':msg.get('id') if isinstance(msg,dict) else None,'error':{'code':-32600,'message':'Invalid Request'}}
        if 'id' not in msg:
            return None
        method, params = msg['method'], msg.get('params',{})
        if method == 'initialize':
            value = {'protocolVersion':'2025-06-18','capabilities':{'tools':{}},'serverInfo':{'name':'agent-'+self.kind,'version':VERSION},'instructions':'Use incremental reads and retain cursors. Fetch details only when needed. Do not repeatedly poll unchanged state. No global read receipts. v2 returns one payload; text JSON by default.'}
            if self.kind == 'gantt':
                value['instructions'] += ' Use ready_only then coordinate claim with task revision before working; complete/release as owner. Summaries are short human labels, descriptions hold full instructions. Parent hierarchy is distinct from dependency order. Actor is a reported label, not authenticated identity.'
        elif method == 'ping':
            value = {}
        elif method == 'tools/list':
            value = {'tools':self.tools()}
        elif method == 'tools/call':
            try:
                if not isinstance(params,dict): raise ValueError('params must be object')
                value = self.result(self.call(params.get('name'),params.get('arguments',{})))
            except Exception as e:
                value = {'content':[{'type':'text','text':str(e)}],'isError':True}
        else:
            return {'jsonrpc':'2.0','id':msg['id'],'error':{'code':-32601,'message':'Method not found'}}
        return {'jsonrpc':'2.0','id':msg['id'],'result':value}


def main(kind_override=None):
    installed_kind = 'gantt' if Path(__file__).resolve().parent.name == 'agent-gantt' else 'chat'
    kind = kind_override or (sys.argv[1] if len(sys.argv)>1 else os.environ.get('AGENT_MCP_KIND',installed_kind))
    default = Path(os.environ.get('XDG_STATE_HOME',Path.home()/'.local/state'))/('agent-'+kind)/('messages.sqlite3' if kind=='chat' else 'projects.sqlite3')
    server = Server(kind,os.environ.get('AGENT_'+kind.upper()+'_DB',default),os.environ.get('AGENT_MCP_TRANSPORT','text'),os.environ.get('AGENT_MCP_PROFILE','full'))
    for line in sys.stdin:
        try:
            response = server.respond(json.loads(line))
        except Exception:
            response = {'jsonrpc':'2.0','id':None,'error':{'code':-32700,'message':'Parse error'}}
        if response is not None: print(dump(response),flush=True)


if __name__ == '__main__':
    main()

