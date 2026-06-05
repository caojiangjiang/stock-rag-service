/**
 * 聊天页：流式 POST /api/chat/stream，SSE 增量渲染
 */
(function (global) {
  let currentConversationID = null;
  let conversations = [];
  let isSending = false;
  let pendingRouteMode = null;

  const chatContainer = () => document.getElementById('chat-container');
  const messageInput = () => document.getElementById('message-input');
  const conversationsList = () => document.getElementById('conversations-list');
  const currentConversationTitle = () => document.getElementById('current-conversation-title');
  const emptyState = () => document.getElementById('empty-state');
  const sendButton = () => document.getElementById('send-button');

  function getCurrentUserID() {
    const user = Auth.getUser();
    return user?.id || user?.user_id || user?.username || 'anonymous';
  }

  function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text == null ? '' : String(text);
    return div.innerHTML;
  }

  function renderMarkdown(text) {
    if (typeof Markdown !== 'undefined') {
      return Markdown.render(text);
    }
    const source = text == null ? '' : String(text);
    return escapeHtml(source).replace(/\n/g, '<br>');
  }

  function setMessageBody(body, content, role, asHtml) {
    body.classList.toggle('markdown-body', role === 'assistant');
    if (role === 'assistant') {
      body.innerHTML = asHtml ? content : renderMarkdown(content);
    } else {
      body.textContent = content;
    }
  }

  function renderUserHeader() {
    const user = Auth.getUser();
    const el = document.getElementById('username-display');
    const avatarEl = document.getElementById('user-avatar');
    if (user && el) {
      el.textContent = user.username;
    }
    if (user && avatarEl) {
      const initial = user.username ? user.username.charAt(0).toUpperCase() : 'U';
      avatarEl.textContent = initial;
    }
  }

  async function loadConversations(autoSelectFirst = true) {
    const res = await Auth.authFetch(`/api/conversations?user_id=${encodeURIComponent(getCurrentUserID())}`);
    if (!res.ok) return;
    conversations = await res.json();
    renderConversations();
    if (autoSelectFirst && conversations.length > 0 && !currentConversationID) {
      await switchConversation(conversations[0].id);
    }
  }

  function renderConversations() {
    const list = conversationsList();
    if (!list) return;
    list.innerHTML = '';

    conversations.forEach((conv) => {
      const item = document.createElement('div');
      item.className = 'conversation-item' + (currentConversationID === conv.id ? ' active' : '');
      item.onclick = () => switchConversation(conv.id);

      const timeStr = conv.updated_at
        ? new Date(conv.updated_at * 1000).toLocaleDateString()
        : '';

      const title = document.createElement('div');
      title.className = 'conversation-title';
      title.textContent = conv.title || '未命名对话';

      const preview = document.createElement('div');
      preview.className = 'conversation-preview';
      preview.textContent = conv.title || '暂无消息';

      const timeEl = document.createElement('div');
      timeEl.className = 'conversation-time';
      timeEl.textContent = timeStr;

      const del = document.createElement('button');
      del.className = 'delete-conv-btn';
      del.type = 'button';
      del.title = '删除';
      del.textContent = '×';
      del.onclick = (e) => {
        e.stopPropagation();
        deleteConversation(conv.id);
      };

      item.append(del, title, preview, timeEl);
      list.appendChild(item);
    });
  }

  async function createNewConversation(title, options = {}) {
    const { skipWelcome = false } = options;
    const convTitle = (title && String(title).trim()) || '未命名对话';
    currentConversationTitle().textContent = convTitle;
    if (skipWelcome) {
      if (emptyState()) emptyState().style.display = 'none';
      chatContainer().innerHTML = '';
    } else {
      showWelcomeMessage('你好！我是 Stock RAG，可以帮你查询和分析股票信息。');
    }

    try {
      const res = await Auth.authFetch('/api/conversations/create', {
        method: 'POST',
        body: JSON.stringify({ user_id: getCurrentUserID(), title: convTitle }),
      });
      if (res.ok) currentConversationID = (await res.json()).id;
    } catch (e) {
      console.error(e);
    }
    await loadConversations(false);
    await loadSessionMemory(currentConversationID);
    messageInput()?.focus();
  }

  function showWelcomeMessage(text) {
    if (emptyState()) emptyState().style.display = 'none';
    chatContainer().innerHTML = '';
    addMessage(text, 'assistant');
  }

  function formatFactValue(value) {
    if (value == null) return '—';
    if (typeof value === 'object') return JSON.stringify(value);
    return String(value);
  }

  async function loadSessionMemory(conversationID) {
    const body = document.getElementById('session-memory-body');
    if (!body) return;
    if (!conversationID) {
      body.innerHTML = '<p class="session-memory-empty">选择对话后显示本会话已确认的事实</p>';
      return;
    }
    body.innerHTML = '<p class="session-memory-empty">加载中…</p>';
    try {
      const session = await MemoryAPI.getSessionMemory(conversationID);
      if (!session.available || !session.confirmed_facts?.length) {
        const hint = session.medium_term_enabled === false
          ? '中期记忆未启用（需 PostgreSQL）'
          : '暂无已确认事实';
        body.innerHTML = `<p class="session-memory-empty">${hint}</p>`;
        return;
      }
      const factsHtml = session.confirmed_facts.map((f) => `
        <div class="session-fact">
          <div class="session-fact-key">${escapeHtml(f.key)}</div>
          <div class="session-fact-value">${escapeHtml(formatFactValue(f.value))}</div>
        </div>
      `).join('');
      const objectsHtml = session.current_objects?.length
        ? `<div class="session-objects">当前关注：<span>${session.current_objects.map(escapeHtml).join('、')}</span></div>`
        : '';
      body.innerHTML = factsHtml + objectsHtml;
    } catch (e) {
      console.error(e);
      body.innerHTML = '<p class="session-memory-empty">加载失败</p>';
    }
  }

  async function switchConversation(conversationID) {
    currentConversationID = conversationID;
    if (emptyState()) emptyState().style.display = 'none';

    const conv = conversations.find((c) => c.id === conversationID);
    if (conv) currentConversationTitle().textContent = conv.title || '未命名对话';

    try {
      const res = await Auth.authFetch(
        `/api/conversations/messages?conversation_id=${encodeURIComponent(conversationID)}`
      );
      chatContainer().innerHTML = '';

      if (res.ok) {
        const messages = await res.json();
        if (messages.length === 0) {
          showWelcomeMessage('欢迎回来！继续我们的对话吧。');
        } else {
          messages.forEach((msg) => {
            addMessage(msg.content, msg.role === 'user' ? 'user' : 'assistant', msg.created_at);
          });
        }
      } else {
        showWelcomeMessage('欢迎回来！继续我们的对话吧。');
      }
    } catch (e) {
      console.error(e);
    }

    renderConversations();
    await loadSessionMemory(conversationID);
    messageInput()?.focus();
  }

  async function deleteConversation(conversationID) {
    if (!confirm('确定要删除这个对话吗？')) return;

    const res = await Auth.authFetch(
      `/api/conversations/delete?conversation_id=${encodeURIComponent(conversationID)}`,
      { method: 'DELETE' }
    );
    if (!res.ok) return;

    conversations = conversations.filter((c) => c.id !== conversationID);
    if (currentConversationID === conversationID) {
      if (conversations.length > 0) {
        await switchConversation(conversations[0].id);
      } else {
        currentConversationID = null;
        currentConversationTitle().textContent = '新对话';
        chatContainer().innerHTML = '';
        emptyState().style.display = 'flex';
        await loadSessionMemory(null);
      }
    }
    renderConversations();
  }

  function addMessage(content, role, timestamp, asHtml) {
    if (emptyState()) emptyState().style.display = 'none';

    const row = document.createElement('div');
    row.className = `message-row ${role === 'user' ? 'user' : 'assistant'}`;

    const bubble = document.createElement('div');
    bubble.className = 'message-bubble';

    const body = document.createElement('div');
    body.className = 'message-content';
    const useHtml =
      asHtml ||
      (role === 'assistant' && typeof content === 'string' && content.includes('class="citations"'));
    setMessageBody(body, content, role, useHtml);

    const meta = document.createElement('div');
    meta.className = 'message-meta';
    meta.textContent = timestamp
      ? new Date(timestamp).toLocaleTimeString()
      : new Date().toLocaleTimeString();

    bubble.append(body, meta);
    row.appendChild(bubble);
    chatContainer().appendChild(row);
    chatContainer().scrollTop = chatContainer().scrollHeight;
  }

  function createStreamingAssistantBubble() {
    removeLoadingMessage();
    if (emptyState()) emptyState().style.display = 'none';

    const row = document.createElement('div');
    row.className = 'message-row assistant streaming-message';

    const bubble = document.createElement('div');
    bubble.className = 'message-bubble';

    const body = document.createElement('div');
    body.className = 'message-content';
    body.textContent = '';

    const meta = document.createElement('div');
    meta.className = 'message-meta';
    meta.textContent = new Date().toLocaleTimeString();

    bubble.append(body, meta);
    row.appendChild(bubble);
    chatContainer().appendChild(row);
    chatContainer().scrollTop = chatContainer().scrollHeight;

    return { row, body, meta };
  }

  function addLoadingMessage() {
    const row = document.createElement('div');
    row.className = 'message-row assistant';
    row.id = 'loading-message';
    row.innerHTML =
      '<div class="message-bubble"><div class="message-content">' +
      '<span class="loading-dots"><span></span><span></span><span></span></span> 正在分析…' +
      '</div></div>';
    chatContainer().appendChild(row);
    chatContainer().scrollTop = chatContainer().scrollHeight;
  }

  function removeLoadingMessage() {
    document.getElementById('loading-message')?.remove();
  }

  function setComposerDisabled(disabled) {
    isSending = disabled;
    const input = messageInput();
    const btn = sendButton();
    if (input) input.disabled = disabled;
    if (btn) btn.disabled = disabled;
  }

  function formatAssistantReply(result) {
    const text = result.content || result.error || '无内容返回';
    let html = renderMarkdown(text);
    if (!result.citations?.length) return html;

    html += '<div class="citations"><h4>引用来源</h4>';
    result.citations.forEach((c, i) => {
      html += `<div class="citation-item">${i + 1}. ${escapeHtml(c.title || '')} (${escapeHtml(c.doc_type || '')})</div>`;
    });
    return html + '</div>';
  }

  function parseSSEBlock(block) {
    let eventType = 'message';
    let dataLine = '';
    for (const line of block.split('\n')) {
      if (line.startsWith('event:')) eventType = line.slice(6).trim();
      else if (line.startsWith('data:')) dataLine += line.slice(5).trim();
    }
    if (!dataLine) return null;
    try {
      return { eventType, payload: JSON.parse(dataLine) };
    } catch {
      return null;
    }
  }

  async function sendRequestStream(message) {
    addLoadingMessage();
    setComposerDisabled(true);

    let streaming = null;
    let fullContent = '';
    let finalResult = null;

    try {
      const res = await Auth.authFetch('/api/chat/stream', {
        method: 'POST',
        body: JSON.stringify({
          message,
          conversation_id: currentConversationID || '',
          user_id: getCurrentUserID(),
          ...(pendingRouteMode ? { mode: pendingRouteMode } : {}),
        }),
      });
      pendingRouteMode = null;

      if (!res.ok) {
        removeLoadingMessage();
        let errMsg = `请求失败 (${res.status})`;
        try {
          const err = await res.json();
          errMsg = err.error || err.message || errMsg;
        } catch {
          /* ignore */
        }
        addMessage(errMsg, 'assistant');
        return;
      }

      const reader = res.body?.getReader();
      if (!reader) {
        removeLoadingMessage();
        addMessage('浏览器不支持流式响应。', 'assistant');
        return;
      }

      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const parts = buffer.split('\n\n');
        buffer = parts.pop() || '';

        for (const part of parts) {
          const parsed = parseSSEBlock(part);
          if (!parsed) continue;

          const { eventType, payload } = parsed;
          if (eventType === 'delta') {
            if (!streaming) streaming = createStreamingAssistantBubble();
            fullContent += payload.content || '';
            streaming.body.classList.add('markdown-body');
            streaming.body.innerHTML = renderMarkdown(fullContent);
            chatContainer().scrollTop = chatContainer().scrollHeight;
          } else if (eventType === 'done') {
            finalResult = payload;
          } else if (eventType === 'error') {
            throw new Error(payload.error || '流式响应失败');
          }
        }
      }

      removeLoadingMessage();

      if (finalResult?.conversation_id) currentConversationID = finalResult.conversation_id;

      if (finalResult) {
        const display = formatAssistantReply({ ...finalResult, content: fullContent || finalResult.content });
        if (streaming) {
          streaming.body.innerHTML = display;
          streaming.body.classList.add('markdown-body');
          streaming.row.classList.remove('streaming-message');
        } else {
          addMessage(display, 'assistant', null, true);
        }
        await loadConversations();
        await loadSessionMemory(currentConversationID);
      } else if (!streaming) {
        addMessage('未收到完整响应，请重试。', 'assistant');
      }
    } catch (err) {
      removeLoadingMessage();
      if (err.message !== 'session expired') {
        addMessage(err.message || '网络异常，请稍后重试。', 'assistant');
      }
    } finally {
      setComposerDisabled(false);
    }
  }

  async function sendMessage() {
    const input = messageInput();
    const text = input?.value.trim();
    if (!text || isSending) return;
    if (!currentConversationID) currentConversationID = 'conversation-' + Date.now();
    
    const isFirstMessage = chatContainer().children.length === 0 || 
                          (chatContainer().children.length === 1 && 
                           chatContainer().children[0].querySelector('.message-content')?.textContent?.includes('你好！我是 Stock RAG'));
    
    addMessage(text, 'user');
    input.value = '';
    input.style.height = 'auto';
    
    if (isFirstMessage) {
      const existingTitle = currentConversationTitle()?.textContent?.trim() || '';
      if (!existingTitle || existingTitle === '新对话' || existingTitle === '未命名对话') {
        await updateConversationTitle(text);
      }
    }
    
    await sendRequestStream(text);
  }

  async function updateConversationTitle(title) {
    if (!currentConversationID) return;
    
    try {
      const res = await Auth.authFetch('/api/conversations', {
        method: 'PUT',
        body: JSON.stringify({ 
          conversation_id: currentConversationID, 
          title: truncateTitle(title) 
        }),
      });
      if (res.ok) {
        currentConversationTitle().textContent = truncateTitle(title);
        await loadConversations();
      }
    } catch (e) {
      console.error('Failed to update conversation title:', e);
    }
  }

  function truncateTitle(title) {
    if (title.length <= 30) return title;
    return title.substring(0, 30) + '...';
  }

  function bindComposer() {
    const input = messageInput();
    sendButton()?.addEventListener('click', sendMessage);
    input?.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        sendMessage();
      }
    });
    input?.addEventListener('input', () => {
      input.style.height = 'auto';
      input.style.height = `${Math.min(input.scrollHeight, 160)}px`;
    });
  }

  async function init() {
    renderUserHeader();
    bindComposer();
    document.getElementById('new-chat-btn')?.addEventListener('click', createNewConversation);
    document.getElementById('logout-btn')?.addEventListener('click', () => Auth.logout());

    const prefill = consumePrefillPrompt();
    const autoSend = sessionStorage.getItem('chat_prefill_autosend') === '1';
    sessionStorage.removeItem('chat_prefill_autosend');
    pendingRouteMode = sessionStorage.getItem('chat_prefill_mode');
    sessionStorage.removeItem('chat_prefill_mode');
    if (prefill) {
      const title = sessionStorage.getItem('chat_prefill_title');
      sessionStorage.removeItem('chat_prefill_title');
      await createNewConversation(title, { skipWelcome: autoSend });
      applyPrefillToInput(prefill);
      if (autoSend) {
        await sendMessage();
      }
    } else {
      await loadConversations(true);
    }
  }

  function consumePrefillPrompt() {
    const text = sessionStorage.getItem('chat_prefill_prompt');
    if (!text) return null;
    sessionStorage.removeItem('chat_prefill_prompt');
    return text;
  }

  function applyPrefillToInput(text) {
    const input = messageInput();
    if (!input) return;
    input.value = text;
    input.dispatchEvent(new Event('input'));
    input.focus();
  }

  global.ChatApp = { init };
})(window);
