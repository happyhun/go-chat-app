/**
 * @file state.js
 * @description Centralized state management for the application.
 * This file exports the shared application state, UI element references, and message type constants.
 */

/**
 * @typedef {object} ClientConfig
 * @property {number} maxChatMessageLength
 * @property {number} maxNicknameLength
 * @property {number} minNicknameLength
 * @property {number} maxRoomIDLength
 */

/**
 * @typedef {object} AppState
 * @property {WebSocket | null} socket - The active WebSocket connection instance.
 * @property {string} myNickname - The current user's nickname.
 * @property {string} myID - The unique ID assigned by the server to the current user.
 * @property {string} roomID - The ID of the room the user is currently in.
 * @property {string} currentView - The currently displayed view ('nickname', 'lobby', or 'chat').
 * @property {number | null} typingTimer - Timer ID for the typing indicator timeout.
 * @property {boolean} isTyping - Flag indicating if the user is currently typing.
 * @property {Object.<string, string>} currentlyTyping - A map of user IDs to nicknames for those currently typing.
 * @property {EventSource | null} lobbyEventSource - The EventSource instance for lobby updates.
 * @property {ClientConfig} config - Configuration values loaded from the server.
 */

/**
 * The shared state of the application.
 * @type {AppState}
 */
export let state = {
  socket: null,
  myNickname: "",
  myID: "",
  roomID: "",
  currentView: "nickname",
  typingTimer: null,
  isTyping: false,
  currentlyTyping: {},
  lobbyEventSource: null,
  config: {
    maxChatMessageLength: 1000,
    maxNicknameLength: 15,
    minNicknameLength: 2,
    maxRoomIDLength: 20,
  },
};

/**
 * A collection of cached UI element references.
 */
export const ui = {
  nicknameContainer: document.getElementById("nickname-container"),
  lobbyContainer: document.getElementById("lobby-container"),
  chatContainer: document.getElementById("chat-container"),
  lobbyLoader: document.getElementById("lobby-loader"),
  nicknameForm: document.getElementById("nickname-form"),
  nicknameInput: document.getElementById("nickname-input"),
  lobbyNickname: document.getElementById("lobby-nickname"),
  roomList: document.getElementById("room-list"),
  createRoomForm: document.getElementById("create-room-form"),
  newRoomInput: document.getElementById("new-room-input"),
  leaveRoomBtn: document.getElementById("leave-room-btn"),
  chatRoomName: document.getElementById("chat-room-name"),
  userCountSpan: document.getElementById("user-count"),
  messagesDiv: document.getElementById("messages"),
  chatForm: document.getElementById("chat-form"),
  messageInput: document.getElementById("message-input"),
  typingIndicator: document.getElementById("typing-indicator"),
  charCounter: document.getElementById("char-counter"),
  lobbyStatusIndicator: document.getElementById("lobby-status-indicator"),
  lobbyStatusText: document.getElementById("lobby-status-text"),
};

/**
 * Constants for WebSocket message types exchanged between client and server.
 */
export const MSG_TYPES = {
  CHAT: "chat",
  USER_JOIN: "user_join",
  USER_LEAVE: "user_leave",
  USER_COUNT: "user_count",
  REGISTER_SUCCESS: "register_success",
  CHAT_ERROR: "chat_error",
  TYPING_START: "typing_start",
  TYPING_STOP: "typing_stop",
  SERVER_SHUTDOWN: "server_shutdown",
  LEAVE_ROOM: "leave_room",
};
