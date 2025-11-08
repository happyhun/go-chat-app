/**
 * @file main.js
 * @description Main entry point for the application. Coordinates UI, API, and WebSocket interactions.
 */

import { MSG_TYPES, state, ui } from "./state.js";
import { checkNicknameAvailability, createRoom, fetchConfig } from "./api.js";
import {
  appendChatMessage,
  displayRooms,
  showLobbyLoader,
  showView,
  updateCharCounter,
} from "./ui.js";
import {
  connectLobbyStream,
  connectWebSocket,
  sendTypingStop,
} from "./websocket.js";

// --- Utility Functions ---

/**
 * Validates an input value based on length and a regex pattern.
 * @param {string} input - The input value to validate.
 * @param {number} min - The minimum allowed length.
 * @param {number} max - The maximum allowed length.
 * @param {RegExp} regex - The regex pattern to test against.
 * @param {string} name - The name of the input field (e.g., "Nickname").
 * @returns {string|null} An error message if validation fails, otherwise null.
 */
const validateInput = (input, min, max, regex, name) => {
  const value = input.trim();
  if (value.length < min || value.length > max) {
    return `${name} must be between ${min} and ${max} characters.`;
  }
  if (!regex.test(value)) {
    return `${name} contains invalid characters.`;
  }
  return null;
};

// --- Core Business Logic ---

/**
 * Displays the lobby view and connects to the lobby's SSE stream.
 */
function showLobby() {
  showView("lobby");
  ui.lobbyNickname.textContent = state.myNickname;
  ui.messagesDiv.innerHTML = ""; // Clear message area when returning to lobby
  connectLobbyStream(state.myNickname);
}

/**
 * Joins a specific chat room by its ID.
 * @param {string} selectedRoomID - The ID of the room to join.
 */
function joinRoom(selectedRoomID) {
  state.roomID = selectedRoomID;
  ui.messagesDiv.innerHTML = ""; // Clear message area for the new room
  showLobbyLoader(true);
  connectWebSocket();
}

/**
 * Handles the submission of the nickname form.
 */
async function handleNicknameSubmit() {
  const nickname = ui.nicknameInput.value.trim();
  const validationError = validateInput(
    nickname,
    state.config.minNicknameLength,
    state.config.maxNicknameLength,
    /^[a-zA-Z0-9가-힣_]+$/,
    "Nickname"
  );

  if (validationError) {
    alert(validationError);
    return;
  }

  const btn = ui.nicknameForm.querySelector("button");
  if (btn.disabled) {
    alert("This nickname is not available. Please choose another one.");
    return;
  }

  state.myNickname = nickname;
  showLobby();
}

/**
 * Submits a chat message to the server via WebSocket.
 */
function submitChatMessage() {
  const message = ui.messageInput.value;
  if (message.length > state.config.maxChatMessageLength) {
    alert(
      `Message cannot exceed ${state.config.maxChatMessageLength} characters.`
    );
    return;
  }
  if (
    message.trim() &&
    state.socket &&
    state.socket.readyState === WebSocket.OPEN
  ) {
    state.socket.send(
      JSON.stringify({ type: MSG_TYPES.CHAT, content: message.trim() })
    );
    sendTypingStop();
    appendChatMessage({
      nickname: state.myNickname,
      content: message.trim(),
      timestamp: new Date().toISOString(),
    });
    ui.messageInput.value = "";
    updateCharCounter();
  }
}

let nicknameCheckTimer;

/**
 * Handles input changes in the nickname field for real-time validation.
 */
async function handleNicknameInput() {
  const nickname = ui.nicknameInput.value;
  const feedbackElement = document.getElementById("nickname-feedback");
  const submitButton = ui.nicknameForm.querySelector("button");

  clearTimeout(nicknameCheckTimer);

  const trimmedNickname = nickname.trim();

  if (trimmedNickname.length === 0) {
    feedbackElement.textContent = "";
    submitButton.disabled = true;
    return;
  }

  const validationError = validateInput(
    trimmedNickname,
    state.config.minNicknameLength,
    state.config.maxNicknameLength,
    /^[a-zA-Z0-9가-힣_]+$/,
    "Nickname"
  );
  if (validationError) {
    feedbackElement.textContent = validationError;
    feedbackElement.className = "text-danger";
    submitButton.disabled = true;
    return;
  }

  feedbackElement.textContent = "Checking...";
  feedbackElement.className = "text-muted";
  submitButton.disabled = true;

  nicknameCheckTimer = setTimeout(async () => {
    try {
      const available = await checkNicknameAvailability(trimmedNickname);
      if (available) {
        feedbackElement.textContent = "Nickname is available.";
        feedbackElement.className = "text-success";
        submitButton.disabled = false;
      } else {
        feedbackElement.textContent = "Nickname is already in use.";
        feedbackElement.className = "text-danger";
        submitButton.disabled = true;
      }
    } catch (error) {
      console.error("Error checking nickname:", error);
      feedbackElement.textContent = "Error checking nickname.";
      feedbackElement.className = "text-danger";
      submitButton.disabled = true;
    }
  }, 300);
}

// --- Event Listeners ---

/**
 * Sets up all DOM event listeners for the application.
 */
function setupEventListeners() {
  // Real-time nickname validation on input.
  ui.nicknameInput.addEventListener("input", handleNicknameInput);

  // Nickname form submission.
  ui.nicknameForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    await handleNicknameSubmit();
  });

  // Create new room form submission.
  ui.createRoomForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const roomID = ui.newRoomInput.value.trim();
    const validationError = validateInput(
      roomID,
      1,
      state.config.maxRoomIDLength,
      /^[a-zA-Z0-9가-힣_-]+$/,
      "Room Name"
    );

    if (validationError) {
      alert(validationError);
      return;
    }

    const btn = e.target.querySelector("button");
    btn.disabled = true;
    btn.textContent = "Creating...";

    try {
      await createRoom(roomID);
      joinRoom(roomID);
    } catch (error) {
      console.error("Error creating or joining room:", error);
      alert(`Error: ${error.message}`);
    } finally {
      btn.disabled = false;
      btn.textContent = "Create & Join";
    }
  });

  // Leave chat room button.
  ui.leaveRoomBtn.addEventListener("click", () => {
    if (state.socket && state.socket.readyState === WebSocket.OPEN) {
      state.socket.send(JSON.stringify({ type: MSG_TYPES.LEAVE_ROOM }));
    }
    showLobby();
  });

  // Chat message form submission.
  ui.chatForm.addEventListener("submit", (e) => {
    e.preventDefault();
    submitChatMessage();
  });

  // Typing indicator logic.
  ui.messageInput.addEventListener("input", () => {
    updateCharCounter();
    if (!state.isTyping) {
      state.isTyping = true;
      state.socket.send(JSON.stringify({ type: MSG_TYPES.TYPING_START }));
    }
    clearTimeout(state.typingTimer);
    state.typingTimer = setTimeout(sendTypingStop, 2000);
  });

  // Custom events for inter-module communication.
  document.addEventListener("join-room", (e) => joinRoom(e.detail));
  document.addEventListener("display-rooms", (e) => displayRooms(e.detail));
  document.addEventListener("show-lobby", showLobby);

  // Reconnect logic when tab becomes visible.
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState !== "visible") return;

    if (
      state.currentView === "lobby" &&
      (!state.lobbyEventSource ||
        state.lobbyEventSource.readyState === EventSource.CLOSED)
    ) {
      console.log("Reconnecting lobby stream...");
      connectLobbyStream(state.myNickname);
    }
    if (
      state.currentView === "chat" &&
      (!state.socket || state.socket.readyState === WebSocket.CLOSED)
    ) {
      console.log("Chat connection lost. Returning to lobby.");
      document.dispatchEvent(new Event("show-lobby"));
    }
  });
}

// --- Application Initialization ---

/**
 * Initializes the application.
 */
async function init() {
  try {
    state.config = await fetchConfig();
    setupEventListeners();
    showView("nickname", ui.nicknameInput);
    ui.nicknameForm.querySelector("button").disabled = true;
  } catch (error) {
    console.error("Initialization failed:", error);
    document.body.innerHTML = `<div style="text-align: center; padding: 2rem; color: #dc3545;"><strong>Initialization Failed:</strong> Could not connect to the server. Please try again later.</div>`;
  }
}

// Start the application once the DOM is fully loaded.
document.addEventListener("DOMContentLoaded", () => {
  init().catch((error) => {
    console.error("Fatal error during application initialization:", error);
  });
});
