/**
 * @file ui.js
 * @description Manages UI rendering and DOM manipulation.
 */

import { state, ui } from "./state.js";

/**
 * @typedef {object} RoomInfo
 * @property {string} id - The ID of the chat room.
 * @property {number} users - The number of users currently in the room.
 */

/**
 * Shows a specific view and hides others.
 * @param {'nickname' | 'lobby' | 'chat'} viewName - The name of the view to show.
 * @param {HTMLElement} [focusElement=null] - The element to focus after the view transition.
 */
export function showView(viewName, focusElement = null) {
  ["nickname", "lobby", "chat"].forEach((id) =>
    ui[`${id}Container`].classList.add("hidden")
  );
  const container = ui[`${viewName}Container`];
  container.classList.remove("hidden");
  state.currentView = viewName;
  if (focusElement) setTimeout(() => focusElement.focus(), 100);
}

/**
 * Toggles the visibility of the lobby loader (spinner).
 * @param {boolean} show - Whether to show the loader.
 */
export function showLobbyLoader(show) {
  ui.lobbyLoader.classList.toggle("hidden", !show);
}

/**
 * Renders the list of chat rooms in the lobby.
 * @param {Array<RoomInfo>} rooms - An array of room information objects.
 */
export function displayRooms(rooms) {
  const roomListEl = ui.roomList;
  if (!roomListEl) {
    console.error("Error: roomList element not found in UI.");
    return;
  }
  roomListEl.innerHTML = ""; // Clear existing list

  if (rooms && rooms.length > 0) {
    rooms.forEach((room) => {
      const card = document.createElement("div");
      card.className = "room-card";
      card.setAttribute("role", "button");
      card.setAttribute("tabindex", "0");
      card.setAttribute(
        "aria-label",
        `Join room ${room.id}, currently ${room.users} users`
      );

      const nameDiv = document.createElement("div");
      nameDiv.className = "room-card-name";
      nameDiv.textContent = room.id;

      const usersDiv = document.createElement("div");
      usersDiv.className = "room-card-users";
      usersDiv.textContent = `${room.users} users`;

      card.appendChild(nameDiv);
      card.appendChild(usersDiv);

      const join = () =>
        document.dispatchEvent(
          new CustomEvent("join-room", { detail: room.id })
        );
      card.onclick = join;
      card.onkeydown = (e) => {
        if (e.key === "Enter" || e.key === " ") join();
      };

      roomListEl.appendChild(card);
    });
  } else {
    const noRoomsDiv = document.createElement("div");
    noRoomsDiv.className = "no-rooms";
    noRoomsDiv.textContent = "No rooms available at the moment.";
    roomListEl.appendChild(noRoomsDiv);
  }
}

/**
 * Appends a chat message to the message display area.
 * @param {object} data - The message data object (containing id, nickname, content, timestamp).
 */
export function appendChatMessage(data) {
  const isMyMessage = data.id ? data.id === state.myID : true;

  const messageElement = document.createElement("div");
  messageElement.className = isMyMessage
    ? "message my-message"
    : "message other-message";

  const contentDiv = document.createElement("div");
  contentDiv.className = "msg-content";
  contentDiv.textContent = data.content;

  const timeSpan = document.createElement("span");
  timeSpan.className = "timestamp";
  timeSpan.textContent = new Date(data.timestamp).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });

  const contentWrapper = document.createElement("div");
  contentWrapper.appendChild(contentDiv);
  contentWrapper.appendChild(timeSpan);

  if (isMyMessage) {
    messageElement.appendChild(contentWrapper);
  } else {
    const nicknameDiv = document.createElement("div");
    nicknameDiv.className = "msg-nickname";
    nicknameDiv.textContent = data.nickname;
    messageElement.appendChild(nicknameDiv);
    messageElement.appendChild(contentWrapper);
  }

  ui.messagesDiv.appendChild(messageElement);
  scrollToBottom();
}

/**
 * Appends a system message to the message display area.
 * @param {string} message - The system message text to display.
 */
export function appendSystemMessage(message) {
  const messageElement = document.createElement("div");
  messageElement.className = "message system-message";
  messageElement.textContent = message;
  ui.messagesDiv.appendChild(messageElement);
  scrollToBottom();
}

/**
 * Updates the typing indicator based on the list of typing users.
 */
export function updateTypingIndicator() {
  const typers = Object.values(state.currentlyTyping);
  if (typers.length === 0) {
    ui.typingIndicator.textContent = "";
    return;
  }
  if (typers.length === 1) {
    ui.typingIndicator.textContent = `${typers[0]} is typing...`;
    return;
  }
  ui.typingIndicator.textContent = `${typers.join(", ")} and ${
    typers.length - 2
  } others are typing...`;
}

/**
 * Updates the character counter for the message input field.
 */
export function updateCharCounter() {
  const count = ui.messageInput.value.length;
  const max = state.config.maxChatMessageLength || 1000;
  ui.charCounter.textContent = `${count} / ${max}`;
  ui.charCounter.classList.toggle("limit-exceeded", count > max);
}

/**
 * Scrolls the message display area to the bottom.
 */
function scrollToBottom() {
  ui.messagesDiv.scrollTop = ui.messagesDiv.scrollHeight;
}
