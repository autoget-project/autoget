function createScrollToTopButton(scrollable: HTMLElement): HTMLElement {
  const button = document.createElement("button");
  button.id = "scroll-to-top-btn";
  button.classList.add("fixed", "bottom-10", "right-10", "btn", "btn-primary", "btn-circle");
  button.setAttribute("aria-label", "Scroll to top");
  button.innerHTML =
    '<span class="icon-[mdi--arrow-up]" style="width: 1.2em; height: 1.2em;"></span>';
  button.onclick = () => {
    scrollable.scrollTo({
      top: 0,
      behavior: "smooth",
    });
  };

  scrollable.appendChild(button);
  return button;
}

export function scrollableOnScroll(scrollable: HTMLElement) {
  let scrollToTopButton = scrollable.querySelector("#scroll-to-top-btn");
  if (!scrollToTopButton) {
    scrollToTopButton = createScrollToTopButton(scrollable);
  }
  if (scrollable.scrollTop > 200) {
    scrollToTopButton.classList.remove("hidden");
  } else {
    scrollToTopButton.classList.add("hidden");
  }
}
