/**
 * @file api.js
 * @description Manages all communication with the server's REST API.
 */

/**
 * @typedef {import('./state.js').ClientConfig} ClientConfig
 */

/**
 * @typedef {object} NicknameAvailabilityResponse
 * @property {boolean} available - Whether the nickname is available.
 */

/**
 * Fetches the client configuration from the server.
 * @returns {Promise<ClientConfig>} A promise that resolves to the configuration object.
 * @throws {Error} If the API call fails.
 */
export async function fetchConfig() {
    const response = await fetch("/api/config");
    if (!response.ok) {
        throw new Error(`Failed to fetch config: ${response.statusText}`);
    }
    return await response.json();
}

/**
 * Checks with the server if a nickname is available.
 * @param {string} nickname - The nickname to check.
 * @returns {Promise<boolean>} A promise that resolves to true if the nickname is available, false otherwise.
 * @throws {Error} If the API call fails or the server returns an error.
 */
export async function checkNicknameAvailability(nickname) {
    const response = await fetch("/api/nicknames/check", {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({nickname}),
    });
    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(errorText || "Server error during nickname check.");
    }
    /** @type {NicknameAvailabilityResponse} */
    const data = await response.json();
    return data.available;
}

/**
 * Requests the server to create a new chat room.
 * @param {string} roomID - The ID for the new room.
 * @returns {Promise<object>} A promise that resolves to the created room information.
 * @throws {Error} If the API call fails, the roomID is invalid, or a room with the same ID already exists.
 */
export async function createRoom(roomID) {
    const response = await fetch("/api/rooms", {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({roomID}),
    });
    if (!response.ok) {
        const errorText = await response.text();
        if (response.status === 409) { // Conflict
            throw new Error(errorText || "A room with this name already exists.");
        }
        throw new Error(errorText || "Server error while creating the room.");
    }
    return await response.json();
}
