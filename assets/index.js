const form = document.getElementById("resource-filter-form")
const url = form.action
const method = form.method
form.addEventListener("submit", async function(e) {
    e.preventDefault();

    const formData = new FormData(e.target);

    try {
        const response = await fetch(url, {
            method: method,
            body: new URLSearchParams(formData),
        });

        notify("fetch done")

        if (!response.ok) throw new Error("서버 에러");
        const result = await response.json();

        const resourceBody = document.getElementById("resource-body")
        resourceBody.innerHTML = result.map(ResourceItemRow).join("")

    } catch (err) {
        console.error("에러:", err);
    }
});

const ResourceItemRow = (resource) =>  (
    `
    <tr>
        <td>${resource.kind}</td>
        <td>${resource.groupResource.Resource}</td>
        <td>${resource.namespace}</td>
        <td>${resource.name}</td>
    </tr>
   `
)

const alert = document.querySelector(".alert")
const notify = (message) => {
    alert.innerText = message
    alert.classList.add("show")
    setTimeout(() => {
        alert.classList.remove("show")
        alert.innerText = ""
    }, 3000)
}