document.getElementById('coordinates-form').addEventListener('submit', async function (event) {
    event.preventDefault();

    const submitButton = document.querySelector('.submit-button');
    const buttonText = document.getElementById('button-text');
    const buttonLoader = document.getElementById('button-loader');
    const output = document.getElementById('output');

    submitButton.disabled = true;
    buttonText.textContent = 'Loading...';
    buttonLoader.style.display = 'inline-block';
    output.value = '';

    const latitude = document.getElementById('latitude').value;
    const longitude = document.getElementById('longitude').value;

    if (!latitude || !longitude) {
        alert('Please enter both latitude and longitude.');

        submitButton.disabled = false;
        buttonText.textContent = 'Submit';
        buttonLoader.style.display = 'none';
        return;
    }

    try {
        const response = await fetch(`/location_weather?lat=${latitude}&lon=${longitude}`);
        const data = await response.json();

        output.value = JSON.stringify(data, null, 2);
    } catch (error) {
        console.error('Error:', error);
        document.getElementById('output').value = 'An error occurred while fetching data.';
    } finally {
        submitButton.disabled = false;
        buttonText.textContent = 'Submit';
        buttonLoader.style.display = 'none';
    }
});