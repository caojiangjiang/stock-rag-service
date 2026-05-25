/**
 * 聊天页：流式 POST /api/chat/stream，SSE 增量渲染
 */
(function (global) {
  let currentConversationID = null;
  let conversations = [];
  let isSending = false;

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
    if (user && el) el.textContent = user.username;
  }

  async function loadConversations() {
    const res = await Auth.authFetch(`/api/conversations?user_id=${encodeURIComponent(getCurrentUserID())}`);
    if (!res.ok) return;
    conversations = await res.json();
    renderConversations();
    if (conversations.length > 0 && !currentConversationID) {
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

  async function createNewConversation() {
    currentConversationTitle().textContent = '未命名对话';
    showWelcomeMessage('你好！我是 Stock RAG，可以帮你查询和分析股票信息。');

    try {
      const res = await Auth.authFetch('/api/conversations/create', {
        method: 'POST',
        body: JSON.stringify({ user_id: getCurrentUserID(), title: '未命名对话' }),
      });
      if (res.ok) currentConversationID = (await res.json()).id;
    } catch (e) {
      console.error(e);
    }
    await loadConversations();
    messageInput()?.focus();
  }

  function showWelcomeMessage(text) {
    if (emptyState()) emptyState().style.display = 'none';
    chatContainer().innerHTML = '';
    addMessage(text, 'assistant');
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
        }),
      });

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
    addMessage(text, 'user');
    input.value = '';
    input.style.height = 'auto';
    await sendRequestStream(text);
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
    await loadConversations();
  }

  global.ChatApp = { init };
})(window);
