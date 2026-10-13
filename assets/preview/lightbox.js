// The lightbox of the Markdown page and the viewer page: an image fitted
// to the viewport, a click toggling fit and 1:1, Escape, the backdrop or
// the ✕ closing it. The viewer page keeps it open and has nothing to
// close to.
(() => {
  const box = document.getElementById("lightbox");
  const img = box.querySelector("img");
  const caption = box.querySelector(".caption");
  const closeButton = box.querySelector(".close");
  let name = "";
  let keep = false; // the viewer page: no closing

  function describe() {
    const kind = (name.split(".").pop() || "").toUpperCase();
    let text = kind;
    if (img.naturalWidth) text += " · " + img.naturalWidth + "×" + img.naturalHeight;
    if (box.classList.contains("actual")) text += " · 1:1";
    else if (img.naturalWidth && img.width < img.naturalWidth) text += " · " + Math.round(100 * img.width / img.naturalWidth) + "%";
    caption.textContent = name + "  —  " + text;
    if (keep) document.title = text;
  }

  window.isImageName = function (href) {
    const p = href.split("#")[0].split("?")[0].toLowerCase();
    return /\.(png|jpe?g|gif|webp|bmp|avif|ico|svg)$/.test(p);
  };

  window.gorexLightbox = function (src, n, stay) {
    name = n || "";
    keep = !!stay;
    box.classList.remove("actual");
    closeButton.style.display = keep ? "none" : "";
    img.src = src;
    box.classList.add("open");
    if (!keep) document.body.style.overflow = "hidden";
    describe();
    return true;
  };

  function close() {
    if (keep) return;
    box.classList.remove("open", "actual");
    document.body.style.overflow = "";
    img.removeAttribute("src");
  }

  img.addEventListener("load", describe);
  window.addEventListener("resize", describe);
  box.addEventListener("click", (e) => {
    if (e.target === closeButton) { close(); return; }
    if (e.target === img) {
      box.classList.toggle("actual");
      describe();
      return;
    }
    close();
  });
  window.addEventListener("keydown", (e) => {
    if (e.key !== "Escape" || !box.classList.contains("open") || keep) return;
    e.preventDefault(); // the preload script's Back stays out of it
    e.stopPropagation();
    close();
  }, true);
})();
