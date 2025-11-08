/**
 * @file websocket.js
 * @description Manages real-time communication using WebSocket and Server-Sent Events (SSE).
 */

import { MSG_TYPES, state, ui } from "./state.js";
import {
  appendChatMessage,
  appendSystemMessage,
  showLobbyLoader,
  showView,
  updateCharCounter,
  updateTypingIndicator,
} from "./ui.js";

// --- Server-Sent Events (Lobby) ---

/**
 * Connects to the SSE stream for real-time lobby updates.
 * @param {string} nickname - The user's nickname to register in the lobby.
 */
export function connectLobbyStream(nickname) {
  if (
    state.lobbyEventSource &&
    state.lobbyEventSource.readyState !== EventSource.CLOSED
  ) {
    console.log("Lobby stream is already connected.");
    return;
  }

  const sseURL = `/api/rooms/stream?nickname=${encodeURIComponent(nickname)}`;
  state.lobbyEventSource = new EventSource(sseURL);

  state.lobbyEventSource.onopen = () => {
    console.log("Lobby stream connected.");
    ui.lobbyStatusIndicator.className = "connected";
    ui.lobbyStatusText.textContent = "Real-time updates";
  };

  state.lobbyEventSource.onmessage = (event) => {
    if (event.data.startsWith("error:")) {
      const errorMessage = event.data.substring(6).trim();
      alert(errorMessage);
      state.lobbyEventSource.close();
      state.myNickname = "";
      showView("nickname", ui.nicknameInput);
      return;
    }

    try {
      const rooms = JSON.parse(event.data);
      document.dispatchEvent(
        new CustomEvent("display-rooms", { detail: rooms })
      );
    } catch (e) {
      console.error("Failed to parse lobby data:", e, "Raw data:", event.data);
    }
  };

  state.lobbyEventSource.onerror = () => {
    console.error("Lobby stream error. The browser will attempt to reconnect.");
    ui.lobbyStatusIndicator.className = "disconnected";
    ui.lobbyStatusText.textContent = "Disconnected";
  };
}

// --- WebSocket (Chat Room) ---

/**
 * Connects to the WebSocket for a specific chat room.
 */
export function connectWebSocket() {
  const protocol = window.location.protocol === "https:" ? "wss" : "ws";
  const socketURL = `${protocol}://${
    window.location.host
  }/ws/${encodeURIComponent(state.roomID)}`;
  state.socket = new WebSocket(socketURL);

  state.socket.onopen = () => {
    state.socket.send(
      JSON.stringify({ type: MSG_TYPES.REGISTER, content: state.myNickname })
    );
  };

  state.socket.onclose = (event) => {
    showLobbyLoader(false);

    // If the client is already in the lobby view, it means the disconnection was intentional
    // (e.g., user clicked "Leave Room"). No further action is needed.
    if (state.currentView === "lobby") {
      console.log("WebSocket closed. Client is already in the lobby.");
      return;
    }

    // Otherwise, the disconnection was unexpected. Notify the user and return to the lobby.
    let messageForUser =
      "Connection to the server has been lost. Returning to the lobby.";
    let delay = 2000;

    if (event.code === 1006) {
      // Abnormal Closure
      messageForUser =
        "Could not connect to the chat room. It may not exist or has been deleted.";
      alert(messageForUser);
      delay = 0; // No delay after a blocking alert
    } else if (state.currentView === "chat") {
      appendSystemMessage(messageForUser);
    }

    setTimeout(() => document.dispatchEvent(new Event("show-lobby")), delay);
  };

  state.socket.onerror = (error) => {
    console.error("WebSocket error:", error);
  };

  state.socket.onmessage = handleSocketMessage;
}

/**
 * Parses and handles incoming WebSocket messages from the server.
 * @param {MessageEvent} event - The WebSocket message event.
 */
function handleSocketMessage(event) {
  const data = JSON.parse(event.data);
  switch (data.type) {
    case MSG_TYPES.REGISTER_SUCCESS:
      state.myID = data.id;
      showLobbyLoader(false);
      showView("chat", ui.messageInput);
      ui.chatRoomName.textContent = state.roomID;
      updateCharCounter();
      break;
    case MSG_TYPES.SERVER_SHUTDOWN:
      appendSystemMessage(data.content);
      state.socket.onclose = null; // Prevent the default onclose handler
      state.socket.close(1000, "Server shutdown received");
      break;
    case MSG_TYPES.REGISTER_ERROR:
      alert(data.content);
      state.socket.close(1000, "Register error received");
      break;
    case MSG_TYPES.CHAT_ERROR:
      alert(data.content);
      break;
    case MSG_TYPES.USER_COUNT:
      ui.userCountSpan.textContent = data.content;
      break;
    case MSG_TYPES.CHAT:
      appendChatMessage(data);
      break;
    case MSG_TYPES.USER_JOIN:
      appendSystemMessage(`'${data.nickname}' has joined.`);
      break;
    case MSG_TYPES.USER_LEAVE:
      appendSystemMessage(`'${data.nickname}' has left.`);
      if (state.currentlyTyping[data.id]) {
        delete state.currentlyTyping[data.id];
        updateTypingIndicator();
      }
      break;
    case MSG_TYPES.TYPING_START:
      if (data.id !== state.myID) {
        state.currentlyTyping[data.id] = data.nickname;
        updateTypingIndicator();
      }
      break;
    case MSG_TYPES.TYPING_STOP:
      if (data.id !== state.myID) {
        delete state.currentlyTyping[data.id];
        updateTypingIndicator();
      }
      break;
    default:
      console.warn("Unknown message type:", data.type);
  }
}

/**
 * Sends a typing stop message to the server.
 */
export function sendTypingStop() {
  if (state.isTyping) {
    state.isTyping = false;
    if (state.socket && state.socket.readyState === WebSocket.OPEN) {
      state.socket.send(JSON.stringify({ type: MSG_TYPES.TYPING_STOP }));
    }
    clearTimeout(state.typingTimer);
  }
}
