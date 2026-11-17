// fallow-ignore-file unused-file -- loaded by layout.html, embedded with go:embed
// Small enhancements for the ai-service admin pages. Every page works
// without this file; it only adds Copy buttons, "are you sure?" checks, text previews,
// Enter-to-send, a "bot is replying" state in the test chat and folding menu groups.
// Rules: never build HTML from strings, no eval, no inline handlers (the CSP allows
// only this file and enforces Trusted Types).
'use strict';
(() => {
  // The few texts this file writes, in the page's language (<html lang>).
  const thai = {
    'Copied': 'คัดลอกแล้ว',
    'Copy failed': 'คัดลอกไม่สำเร็จ',
    'Test conversation': 'บทสนทนาทดสอบ',
    'You (as customer)': 'คุณ (ในฐานะลูกค้า)',
    'Attached:': 'แนบ:',
    'Bot is replying': 'บอตกำลังตอบ',
    'Unsaved changes': 'ยังไม่ได้บันทึก',
  };
  const t = (text) => (document.documentElement.lang === 'th' && thai[text]) || text;
  const status = document.createElement('p');
  status.className = 'visually-hidden';
  status.setAttribute('role', 'status');
  document.body.append(status);

  const say = (button, text) => {
    const label = button.dataset.label || button.textContent;
    button.dataset.label = label;
    button.textContent = text;
    status.textContent = text;
    setTimeout(() => { button.textContent = label; }, 1500);
  };

  // <button type="button" data-copy="ID" hidden>Copy</button> copies the
  // value of an input, or the text of any other element, with that id.
  if (navigator.clipboard) {
    for (const button of document.querySelectorAll('button[data-copy]')) {
      const target = document.getElementById(button.dataset.copy);
      if (!target) continue;
      button.hidden = false;
      button.addEventListener('click', async () => {
        const text = target instanceof HTMLInputElement ? target.value : target.textContent;
        try {
          await navigator.clipboard.writeText(text);
          say(button, t('Copied'));
        } catch {
          say(button, t('Copy failed'));
        }
      });
    }
  }

  // <textarea data-enter-send>: Enter sends the form, Shift+Enter adds a new
  // line. Enter that finishes a word in an input method (Thai, Japanese, ...)
  // is left alone, and an empty box with no file chosen does not send.
  for (const box of document.querySelectorAll('textarea[data-enter-send]')) {
    const hintEl = box.dataset.enterSend && document.getElementById(box.dataset.enterSend);
    if (hintEl) {
      hintEl.hidden = false;
      box.setAttribute('aria-describedby', hintEl.id);
    }
    box.addEventListener('keydown', (event) => {
      if (event.key !== 'Enter' || event.shiftKey || event.altKey || event.ctrlKey || event.metaKey) return;
      if (event.isComposing || event.keyCode === 229) return;
      event.preventDefault();
      const form = box.form;
      if (!form) return;
      const files = form.querySelector('input[type=file]');
      if (box.value.trim() === '' && !(files && files.files && files.files.length)) return;
      form.requestSubmit();
    });
  }

  // <form data-busy="Sending…">: while the server works on the reply, show the
  // customer's message and a typing bubble at once, and block a second send.
  // The page itself is still a plain form post.
  const el = (tag, className, text) => {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text) node.textContent = text;
    return node;
  };
  const showPending = (form) => {
    const section = form.closest('section');
    if (!section) return;
    let chat = section.querySelector('.chat');
    if (!chat) {
      chat = el('div', 'chat');
      chat.setAttribute('role', 'log');
      chat.setAttribute('aria-label', t('Test conversation'));
      const empty = section.querySelector('.empty');
      if (empty) empty.replaceWith(chat); else form.before(chat);
    }
    const box = form.querySelector('textarea');
    const text = box ? box.value.trim() : '';
    const files = form.querySelector('input[type=file]');
    const names = files && files.files ? Array.from(files.files, (f) => f.name) : [];
    const mine = el('div', 'bubble user pending');
    mine.append(el('span', 'who', t('You (as customer)')));
    if (text) mine.append(text);
    if (names.length) mine.append((text ? '\n' : '') + t('Attached:') + ' ' + names.join(', '));
    const typing = el('div', 'bubble assistant typing pending');
    typing.setAttribute('role', 'status');
    const dots = el('span', 'dots');
    dots.setAttribute('aria-hidden', 'true');
    dots.append(el('span'), el('span'), el('span'));
    typing.append(dots, el('span', '', t('Bot is replying')));
    chat.append(mine, typing);
    typing.scrollIntoView({ block: 'nearest' });
  };
  document.addEventListener('submit', (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement) || !form.dataset.busy || event.defaultPrevented) return;
    if (form.getAttribute('aria-busy') === 'true') {
      event.preventDefault();
      return;
    }
    form.setAttribute('aria-busy', 'true');
    for (const button of form.querySelectorAll('button[type=submit]')) {
      button.dataset.label = button.textContent;
      button.textContent = form.dataset.busy;
      button.disabled = true;
    }
    // Read-only, not disabled: a disabled field would not be sent.
    for (const box of form.querySelectorAll('textarea')) box.readOnly = true;
    showPending(form);
  });
  // Back/forward can restore the page as it was left: undo the busy state.
  window.addEventListener('pageshow', (event) => {
    if (!event.persisted) return;
    for (const form of document.querySelectorAll('form[data-busy][aria-busy]')) {
      form.removeAttribute('aria-busy');
      for (const box of form.querySelectorAll('textarea')) box.readOnly = false;
      for (const button of form.querySelectorAll('button[type=submit]')) {
        button.disabled = false;
        if (button.dataset.label) button.textContent = button.dataset.label;
      }
    }
    for (const node of document.querySelectorAll('.bubble.pending')) node.remove();
  });

  // Phone menu (<details class="menu">): close it when focus moves on to the
  // page below, so nothing focused hides under the open panel, and on Escape.
  for (const menu of document.querySelectorAll('details.menu')) {
    menu.addEventListener('focusout', (event) => {
      // relatedTarget is null for taps that do not move focus (Safari): keep it open then.
      if (menu.open && event.relatedTarget && !menu.contains(event.relatedTarget)) menu.open = false;
    });
    // Focus that lands outside it without leaving it first (a tap on the page) closes it too.
    document.addEventListener('focusin', (event) => {
      if (menu.open && !menu.contains(event.target)) menu.open = false;
    });
    menu.addEventListener('keydown', (event) => {
      if (event.key !== 'Escape' || !menu.open) return;
      menu.open = false;
      menu.querySelector('summary').focus();
    });
  }

  // <textarea data-preview="ID" data-count="ID">: show the text as typed in
  // the preview element and its length (characters, as the server counts)
  // in the counter. Text only (textContent), never HTML.
  const livePreview = (box) => {
    const preview = document.getElementById(box.dataset.preview);
    const count = document.getElementById(box.dataset.count || '');
    box.addEventListener('input', () => {
      const text = box.value.trim();
      if (preview) preview.textContent = text;
      if (count) count.textContent = String([...text].length);
    });
  };
  document.querySelectorAll('textarea[data-preview]').forEach(livePreview);

  // <form data-confirm="Question?"> asks before it is sent.
  document.addEventListener('submit', (event) => {
    const form = event.target;
    if (form instanceof HTMLFormElement && form.dataset.confirm && !window.confirm(form.dataset.confirm)) {
      event.preventDefault();
    }
  });

  // Menu groups fold when their heading is clicked. The folded ones are kept
  // in the admin_nav cookie ("bot.settings") so the next page draws them
  // folded; the sidebar and the phone menu stay in step.
  const navGroups = document.querySelectorAll('details.nav-group[data-group]');
  const foldedNow = () => {
    const m = document.cookie.match(/(?:^|;\s*)admin_nav=([a-z.]*)/);
    return new Set(m && m[1] ? m[1].split('.') : []);
  };
  for (const group of navGroups) {
    // Only a click on the heading counts: the browser also fires "toggle"
    // while drawing the page, and the open page's group is always drawn open.
    group.querySelector('summary')?.addEventListener('click', () => { group.dataset.userToggle = '1'; });
    group.addEventListener('toggle', () => {
      if (!group.dataset.userToggle) return;
      delete group.dataset.userToggle;
      for (const twin of navGroups) {
        if (twin !== group && twin.dataset.group === group.dataset.group) twin.open = group.open;
      }
      const folded = foldedNow();
      if (group.open) folded.delete(group.dataset.group); else folded.add(group.dataset.group);
      const secure = location.protocol === 'https:' ? '; secure' : '';
      document.cookie = 'admin_nav=' + [...folded].join('.') + '; path=/admin; max-age=31536000; samesite=strict' + secure;
    });
  }

  // The account dropdown opens the picked account at once; its Open button
  // stays only for browsers without scripts.
  for (const box of document.querySelectorAll('select[data-autosubmit]')) {
    box.form?.classList.add('auto');
    box.addEventListener('change', () => box.form?.requestSubmit());
  }

  // Unsaved changes. A form that saves (method=post, with fields a person
  // fills in) is compared with how the page drew it: while it differs, an
  // "Unsaved changes" note shows by its button and leaving the page asks
  // first (the browser's own "Leave site?" box). A form the server sent back
  // with "Not saved." (data-unsaved) counts as changed from the start.
  // <form data-no-warn> opts out (the test chat).
  const fieldText = (value) => (typeof value === 'string' ? value : value.name + ':' + value.size);
  const fieldsOf = (form) => [...new FormData(form)].map(([name, value]) => name + '=' + fieldText(value)).join('&');
  const fillable = 'input:not([type=hidden]):not([type=submit]):not([type=button]), textarea, select';
  const savingForms = [...document.querySelectorAll('form[method=post]:not([data-no-warn])')]
    .filter((form) => form.querySelector(fillable));
  const drawn = new Map(savingForms.map((form) => [form, fieldsOf(form)]));
  const formAfter = (alert) => {
    const area = alert.closest('section, .card') || alert.parentElement;
    return savingForms.find((form) => area.contains(form) && (alert.compareDocumentPosition(form) & Node.DOCUMENT_POSITION_FOLLOWING));
  };
  const markRejected = (alert) => {
    const form = formAfter(alert);
    if (form) drawn.set(form, null);
  };
  document.querySelectorAll('[data-unsaved]').forEach(markRejected);
  const changed = (form) => drawn.get(form) === null || fieldsOf(form) !== drawn.get(form);
  const notes = new Map();
  const noteFor = (form) => {
    if (notes.has(form)) return notes.get(form);
    const buttons = form.querySelectorAll('button[type=submit], button:not([type])');
    const last = buttons[buttons.length - 1];
    const note = last ? el('span', 'unsaved-note', t('Unsaved changes')) : null;
    if (note) last.after(note);
    notes.set(form, note);
    return note;
  };
  const showNote = (form) => {
    const note = noteFor(form);
    if (note) note.hidden = !changed(form);
  };
  savingForms.filter((form) => drawn.get(form) === null).forEach(showNote);
  const onEdit = (event) => {
    const form = event.target.form;
    if (drawn.has(form)) showNote(form);
  };
  document.addEventListener('input', onEdit);
  document.addEventListener('change', onEdit);
  let sending = null;
  document.addEventListener('submit', (event) => {
    if (!event.defaultPrevented) sending = event.target;
  });
  window.addEventListener('beforeunload', (event) => {
    const unsaved = savingForms.some((form) => form !== sending && changed(form));
    sending = null;
    if (!unsaved) return;
    event.preventDefault();
    event.returnValue = '';
  });

  // Save messages (data-flash) show once: done=/saved= leave the address so a
  // reload or a bookmark does not repeat them. When the page opens scrolled
  // to an item (a knowledge edit jumps to its row), the message is out of
  // sight, so it floats at the bottom for six seconds.
  const flash = document.querySelector('[data-flash]');
  const dropDoneFromAddress = () => {
    const address = new URL(location.href);
    if (!address.searchParams.has('done') && !address.searchParams.has('saved')) return;
    for (const key of ['done', 'saved', 'chunks', 'count', 'outcome']) address.searchParams.delete(key);
    history.replaceState(history.state, '', address);
  };
  const floatIfOutOfSight = () => {
    const box = flash.getBoundingClientRect();
    if (box.bottom > 0 && box.top < window.innerHeight) return;
    flash.classList.add('toast');
    setTimeout(() => flash.classList.remove('toast'), 6000);
  };
  const showSavedOnce = () => {
    if (!flash) return;
    dropDoneFromAddress();
    window.addEventListener('load', floatIfOutOfSight);
  };
  showSavedOnce();
})();
